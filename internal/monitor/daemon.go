package monitor

import (
	"context"
	"fmt"
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

	// Set action callback for pause/kill notifications
	if cfg.OnAction != nil {
		responder.SetOnAction(cfg.OnAction)
	}

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

			// Detect threats
			threats := d.detector.Analyze(snapshot)
			snapshot.Threats = threats

			// Log snapshot to audit log
			if err := d.auditLog.WriteSnapshot(snapshot); err != nil {
				if d.config.OnError != nil {
					d.config.OnError(fmt.Errorf("audit log write failed: %w", err))
				}
			}

			// Handle threats
			for _, threat := range threats {
				if err := d.responder.Handle(threat); err != nil {
					if d.config.OnError != nil {
						d.config.OnError(fmt.Errorf("threat response failed: %w", err))
					}
				}

				// If container was killed, stop monitoring
				if threat.Action == "killed" {
					return
				}
			}

		case <-d.ctx.Done():
			return
		}
	}
}

// Stop gracefully stops the monitoring daemon
func (d *Daemon) Stop() error {
	d.cancel()

	// Wait for daemon to finish (with timeout)
	select {
	case <-d.done:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("daemon shutdown timeout")
	}
}
