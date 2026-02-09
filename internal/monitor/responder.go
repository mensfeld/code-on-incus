package monitor

import (
	"fmt"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// Responder handles automated responses to threats
type Responder struct {
	containerName      string
	autoPauseOnHigh    bool
	autoKillOnCritical bool
	auditLog           *AuditLog
	onThreat           func(ThreatEvent)
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
	}
}

// Handle processes a threat and takes appropriate action
func (r *Responder) Handle(threat ThreatEvent) error {
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
			threat.Action = "paused"
			r.alert(threat)
			if err := r.logThreat(threat); err != nil {
				return err
			}
			return r.pauseContainer()
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
			return r.killContainer()
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
func (r *Responder) pauseContainer() error {
	_, err := container.IncusOutput("pause", r.containerName)
	if err != nil {
		return fmt.Errorf("failed to pause container: %w", err)
	}
	return nil
}

// killContainer stops and deletes the container
func (r *Responder) killContainer() error {
	// First stop the container
	_, err := container.IncusOutput("stop", r.containerName, "--force")
	if err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	// Then delete it
	_, err = container.IncusOutput("delete", r.containerName)
	if err != nil {
		return fmt.Errorf("failed to delete container: %w", err)
	}

	return nil
}
