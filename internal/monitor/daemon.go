package monitor

import (
	"context"
	"fmt"
	"log"
	"time"
)

// Daemon runs the monitoring loop in the background
type Daemon struct {
	ctx       context.Context
	cancel    context.CancelFunc
	config    DaemonConfig
	collector *Collector
	detector  *Detector
	responder *Responder
	auditLog  *AuditLog
	done      chan struct{}
}

// StartDaemon creates and starts a monitoring daemon
func StartDaemon(ctx context.Context, cfg DaemonConfig) (*Daemon, error) {
	// Create audit log
	auditLog, err := NewAuditLog(cfg.AuditLogPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit log: %w", err)
	}

	// Create daemon context
	daemonCtx, cancel := context.WithCancel(ctx)

	// Create components
	collector := NewCollector(cfg.ContainerName, "", cfg.WorkspacePath, cfg.AllowedCIDRs)
	detector := NewDetector(cfg.FileReadThresholdMB, cfg.FileReadRateMBPerSec)
	responder := NewResponder(cfg.ContainerName, cfg.AutoPauseOnHigh, cfg.AutoKillOnCritical,
		auditLog, cfg.OnThreat)

	daemon := &Daemon{
		ctx:       daemonCtx,
		cancel:    cancel,
		config:    cfg,
		collector: collector,
		detector:  detector,
		responder: responder,
		auditLog:  auditLog,
		done:      make(chan struct{}),
	}

	// Start monitoring loop in background
	go daemon.run()

	return daemon, nil
}

// run is the main monitoring loop
func (d *Daemon) run() {
	defer close(d.done)
	defer d.auditLog.Close()

	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()

	log.Printf("[monitor] Starting monitoring daemon for container %s", d.config.ContainerName)

	for {
		select {
		case <-ticker.C:
			// Collect snapshot
			snapshot, err := d.collector.Collect(d.ctx)
			if err != nil {
				if d.config.OnError != nil {
					d.config.OnError(fmt.Errorf("collection failed: %w", err))
				}
				continue
			}

			// DEBUG: Log process count
			log.Printf("[monitor] Collected %d processes", len(snapshot.Processes.Processes))

			// Detect threats
			threats := d.detector.Analyze(snapshot)
			snapshot.Threats = threats

			// DEBUG: Log threat detection results
			log.Printf("[monitor] Analyzer returned %d threats", len(threats))
			for i, threat := range threats {
				log.Printf("[monitor] Threat %d: level=%s category=%s title=%q",
					i, threat.Level, threat.Category, threat.Title)
			}

			// Log snapshot to audit log
			if err := d.auditLog.WriteSnapshot(snapshot); err != nil {
				if d.config.OnError != nil {
					d.config.OnError(fmt.Errorf("audit log write failed: %w", err))
				}
			}

			// Handle threats
			for _, threat := range threats {
				log.Printf("[monitor] Calling responder.Handle for threat: %s", threat.Title)
				if err := d.responder.Handle(threat); err != nil {
					log.Printf("[monitor] ERROR handling threat: %v", err)
					if d.config.OnError != nil {
						d.config.OnError(fmt.Errorf("threat response failed: %w", err))
					}
				} else {
					log.Printf("[monitor] Threat handled successfully, action=%s", threat.Action)
				}

				// If container was killed, stop monitoring
				if threat.Action == "killed" {
					log.Printf("[monitor] Container killed due to critical threat, stopping daemon")
					return
				}
			}

		case <-d.ctx.Done():
			log.Printf("[monitor] Monitoring daemon stopped for container %s", d.config.ContainerName)
			return
		}
	}
}

// Stop gracefully stops the monitoring daemon
func (d *Daemon) Stop() error {
	log.Printf("[monitor] Stopping monitoring daemon for container %s", d.config.ContainerName)
	d.cancel()

	// Wait for daemon to finish (with timeout)
	select {
	case <-d.done:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("daemon shutdown timeout")
	}
}
