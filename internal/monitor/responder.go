package monitor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/network"
)

// Responder handles automated responses to threats
type Responder struct {
	containerName      string
	autoPauseOnHigh    bool
	autoKillOnCritical bool
	auditLog           *AuditLog
	onThreat           func(ThreatEvent)
	onAction           func(action, message string) // Called when container is paused/killed

	// State tracking to prevent infinite loops
	mu            sync.Mutex
	paused        bool
	killed        bool
	recentThreats map[string]time.Time // threat key -> last alert time
	dedupeWindow  time.Duration
}

// NewResponder creates a new threat responder
func NewResponder(containerName string, autoPauseOnHigh, autoKillOnCritical bool,
	auditLog *AuditLog, onThreat func(ThreatEvent),
) *Responder {
	return &Responder{
		containerName:      containerName,
		autoPauseOnHigh:    autoPauseOnHigh,
		autoKillOnCritical: autoKillOnCritical,
		auditLog:           auditLog,
		onThreat:           onThreat,
		recentThreats:      make(map[string]time.Time),
		dedupeWindow:       30 * time.Second, // Don't re-alert for same threat within 30s
	}
}

// SetOnAction sets a callback for when critical actions (pause/kill) are taken
func (r *Responder) SetOnAction(callback func(action, message string)) {
	r.onAction = callback
}

// Handle processes a threat and takes appropriate action
func (r *Responder) Handle(ctx context.Context, threat ThreatEvent) error {
	r.mu.Lock()

	// If already killed, nothing more to do
	if r.killed {
		r.mu.Unlock()
		return nil
	}

	// Deduplicate recent threats - create a key from threat category and title
	threatKey := threat.Category + ":" + threat.Title
	if s := threat.Evidence.String(); s != "" {
		// Include evidence summary in key for more precise deduplication
		threatKey += ":" + s
	}

	now := time.Now()
	if lastSeen, exists := r.recentThreats[threatKey]; exists {
		if now.Sub(lastSeen) < r.dedupeWindow {
			// Already alerted for this threat recently, just log silently
			r.mu.Unlock()
			threat.Action = "deduplicated"
			return r.logThreat(threat)
		}
	}
	r.recentThreats[threatKey] = now

	// Clean up old entries from the map periodically
	if len(r.recentThreats) > 100 {
		for key, ts := range r.recentThreats {
			if now.Sub(ts) > r.dedupeWindow*2 {
				delete(r.recentThreats, key)
			}
		}
	}

	// Check if already paused (for high-level threats that would pause)
	alreadyPaused := r.paused
	r.mu.Unlock()

	// Determine action based on threat level
	switch threat.Level {
	case ThreatLevelInfo:
		threat.Action = "logged"
		return r.logThreat(threat)

	case ThreatLevelWarning:
		threat.Action = "alerted"
		r.alert(threat)
		return r.logThreat(threat)

	case ThreatLevelHigh:
		if r.autoPauseOnHigh {
			if alreadyPaused {
				// Already paused, just log
				threat.Action = "logged (already paused)"
				return r.logThreat(threat)
			}
			threat.Action = "paused"
			r.alert(threat)
			if err := r.logThreat(threat); err != nil {
				return err
			}
			return r.pauseContainer(ctx)
		}
		threat.Action = "alerted"
		r.alert(threat)
		return r.logThreat(threat)

	case ThreatLevelCritical:
		if r.autoKillOnCritical {
			threat.Action = "killed"
			r.alert(threat)
			if err := r.logThreat(threat); err != nil {
				return err
			}
			return r.killContainer(ctx)
		}
		threat.Action = "alerted"
		r.alert(threat)
		return r.logThreat(threat)
	}

	return nil
}

// logThreat writes threat to audit log
func (r *Responder) logThreat(threat ThreatEvent) error {
	if r.auditLog != nil {
		return r.auditLog.WriteThreat(threat)
	}
	return nil
}

// alert notifies via callback
func (r *Responder) alert(threat ThreatEvent) {
	if r.onThreat != nil {
		r.onThreat(threat)
	}
}

// pauseContainer pauses the container
func (r *Responder) pauseContainer(ctx context.Context) error {
	r.mu.Lock()
	if r.paused {
		r.mu.Unlock()
		return nil // Already paused
	}
	r.mu.Unlock()

	// Use IncusOutputWithStderr to capture error messages from Incus
	// (like "already frozen" which goes to stderr)
	output, err := container.IncusOutputWithStderrContext(ctx, "pause", r.containerName)
	if err != nil {
		// Check if error is because container is already paused
		// Incus returns "The container is already frozen" for this case
		// The message may be in err.Error() or in the combined output
		errStr := err.Error() + " " + output
		if strings.Contains(errStr, "already frozen") ||
			strings.Contains(errStr, "already paused") {
			r.mu.Lock()
			r.paused = true
			r.mu.Unlock()
			return nil
		}
		return fmt.Errorf("failed to pause container: %w", err)
	}

	r.mu.Lock()
	r.paused = true
	r.mu.Unlock()

	// Notify about the pause action
	if r.onAction != nil {
		r.onAction("paused", fmt.Sprintf("Container %s PAUSED due to security threat. Unfreeze with: coi unfreeze %s", r.containerName, r.containerName))
	}

	return nil
}

// killContainer stops and deletes the container
func (r *Responder) killContainer(ctx context.Context) error {
	r.mu.Lock()
	if r.killed {
		r.mu.Unlock()
		return nil // Already killed
	}
	r.mu.Unlock()

	// Notify about the kill action BEFORE killing
	if r.onAction != nil {
		r.onAction("killed", fmt.Sprintf("Container %s KILLED due to critical security threat", r.containerName))
	}

	// Get container IP and veth name BEFORE stopping (needed for cleanup)
	var containerIP string
	var vethName string
	if network.FirewallAvailable() {
		containerIP, _ = network.GetContainerIPFast(r.containerName)
		vethName, _ = network.GetContainerVethName(r.containerName)
	}

	// First stop the container
	_, err := container.IncusOutputContext(ctx, "stop", r.containerName, "--force")
	if err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	// Clean up firewall and NFT monitoring rules BEFORE deleting container
	if containerIP != "" {
		if err := r.cleanupFirewallRules(containerIP); err != nil {
			// Log warning but don't fail the kill operation
			fmt.Printf("Warning: Failed to cleanup firewall rules: %v\n", err)
		}
		if err := r.cleanupNFTRules(containerIP); err != nil {
			// Log warning but don't fail the kill operation
			fmt.Printf("Warning: Failed to cleanup NFT monitoring rules: %v\n", err)
		}
	}

	// Then delete it
	_, err = container.IncusOutputContext(ctx, "delete", r.containerName)
	if err != nil {
		return fmt.Errorf("failed to delete container: %w", err)
	}

	// Clean up firewalld zone binding for the veth interface AFTER container deletion
	if vethName != "" {
		if err := network.RemoveVethFromFirewalldZone(vethName); err != nil {
			fmt.Printf("Warning: Failed to cleanup firewalld zone binding: %v\n", err)
		}
	}

	r.mu.Lock()
	r.killed = true
	r.mu.Unlock()
	return nil
}

// cleanupFirewallRules removes firewall rules for a container IP
func (r *Responder) cleanupFirewallRules(containerIP string) error {
	fm := network.NewFirewallManager(containerIP, "")
	return fm.RemoveRules()
}

// cleanupNFTRules removes NFT monitoring rules for a container IP
func (r *Responder) cleanupNFTRules(containerIP string) error {
	return network.CleanupNFTMonitoringRules(containerIP)
}
