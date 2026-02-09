//go:build linux
// +build linux

package nftmonitor

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/mensfeld/code-on-incus/internal/monitor"
)

// Daemon orchestrates nftables-based network monitoring
type Daemon struct {
	config      *Config
	ruleManager *RuleManager
	logReader   *LogReader
	detector    *NetworkDetector
	responder   *monitor.Responder
	auditLog    *monitor.AuditLog
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.Mutex
	running     bool
}

// StartDaemon starts the nftables monitoring daemon
func StartDaemon(ctx context.Context, cfg Config) (*Daemon, error) {
	// Create audit log
	auditLog, err := monitor.NewAuditLog(cfg.AuditLogPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit log: %w", err)
	}

	// Create responder (convert OnThreat callback)
	responder := monitor.NewResponder(
		cfg.ContainerName,
		true, // autoPauseOnHigh
		true, // autoKillOnCritical
		auditLog,
		func(threat monitor.ThreatEvent) {
			if cfg.OnThreat != nil {
				// Convert monitor.ThreatEvent to nftmonitor.ThreatEvent
				cfg.OnThreat(ThreatEvent{
					Timestamp:   threat.Timestamp,
					Level:       ThreatLevel(threat.Level),
					Category:    threat.Category,
					Title:       threat.Title,
					Description: threat.Description,
					Evidence:    threat.Evidence,
				})
			}
		},
	)

	// Create daemon context
	daemonCtx, cancel := context.WithCancel(ctx)

	daemon := &Daemon{
		config:      &cfg,
		ruleManager: NewRuleManager(&cfg),
		detector:    NewNetworkDetector(&cfg),
		responder:   responder,
		auditLog:    auditLog,
		ctx:         daemonCtx,
		cancel:      cancel,
		running:     false,
	}

	// Create log reader
	logReader, err := NewLogReader(&cfg)
	if err != nil {
		cancel()
		auditLog.Close()
		return nil, fmt.Errorf("failed to create log reader: %w", err)
	}
	daemon.logReader = logReader

	// Add nftables rules
	if err := daemon.ruleManager.AddRules(); err != nil {
		cancel()
		auditLog.Close()
		return nil, fmt.Errorf("failed to add nftables rules: %w", err)
	}

	// Start monitoring goroutines
	daemon.start()

	return daemon, nil
}

// start begins the monitoring loops
func (d *Daemon) start() {
	d.mu.Lock()
	d.running = true
	d.mu.Unlock()

	// Start log reader
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if err := d.logReader.Start(d.ctx); err != nil {
			if d.ctx.Err() == nil {
				fmt.Printf("Log reader error: %v\n", err)
			}
		}
	}()

	// Start event processor
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.processEvents()
	}()
}

// processEvents processes network events and detects threats
func (d *Daemon) processEvents() {
	for {
		select {
		case <-d.ctx.Done():
			return
		case event := <-d.logReader.Events():
			if event == nil {
				continue
			}

			// Analyze event for threats
			if threat := d.detector.Analyze(event); threat != nil {
				// Convert to monitor.ThreatEvent
				monitorThreat := monitor.ThreatEvent{
					ID:          uuid.New().String(),
					Timestamp:   threat.Timestamp,
					Level:       monitor.ThreatLevel(threat.Level),
					Category:    threat.Category,
					Title:       threat.Title,
					Description: threat.Description,
					Evidence:    threat.Evidence,
					Action:      "pending",
				}

				// Handle threat (logging, alerting, response)
				if err := d.responder.Handle(monitorThreat); err != nil {
					fmt.Printf("Error handling threat: %v\n", err)
				}
			}
		}
	}
}

// Stop stops the daemon and cleans up resources
func (d *Daemon) Stop() error {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return nil
	}
	d.running = false
	d.mu.Unlock()

	// Cancel context to stop goroutines
	d.cancel()

	// Wait for goroutines to finish
	d.wg.Wait()

	// Close log reader
	if d.logReader != nil {
		d.logReader.Close()
	}

	// Remove nftables rules
	var ruleErr error
	if d.ruleManager != nil {
		ruleErr = d.ruleManager.RemoveRules()
	}

	// Close audit log
	var auditErr error
	if d.auditLog != nil {
		auditErr = d.auditLog.Close()
	}

	// Return first error if any
	if ruleErr != nil {
		return fmt.Errorf("failed to remove nftables rules: %w", ruleErr)
	}
	if auditErr != nil {
		return fmt.Errorf("failed to close audit log: %w", auditErr)
	}

	return nil
}

// IsRunning returns whether the daemon is running
func (d *Daemon) IsRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}
