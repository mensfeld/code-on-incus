package limits

import (
	"context"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/logger"
)

// TimeoutMonitor monitors a container's runtime and stops it when max duration is reached
type TimeoutMonitor struct {
	ContainerName string
	MaxDuration   time.Duration
	AutoStop      bool
	StopGraceful  bool
	Project       string
	Logger        *logger.SessionLogger

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// NewTimeoutMonitor creates a new timeout monitor.
// The monitor's internal context is derived from ctx, so cancelling ctx also
// stops the monitor (in addition to calling Stop explicitly).
func NewTimeoutMonitor(ctx context.Context, containerName string, maxDuration time.Duration, autoStop, stopGraceful bool, project string, log *logger.SessionLogger) *TimeoutMonitor {
	monCtx, cancel := context.WithCancel(ctx)
	return &TimeoutMonitor{
		ContainerName: containerName,
		MaxDuration:   maxDuration,
		AutoStop:      autoStop,
		StopGraceful:  stopGraceful,
		Project:       project,
		Logger:        log,
		ctx:           monCtx,
		cancel:        cancel,
		done:          make(chan struct{}),
	}
}

// Start starts the timeout monitor in a background goroutine
// Returns immediately - the monitor runs in the background
func (tm *TimeoutMonitor) Start() {
	if tm.MaxDuration == 0 {
		// No limit configured
		close(tm.done)
		return
	}

	tm.Logger.Printf("[limits] Container will auto-stop after %s", tm.MaxDuration)

	go tm.run()
}

// run is the main monitoring loop (runs in background goroutine)
func (tm *TimeoutMonitor) run() {
	defer close(tm.done)

	// Create a timer for the max duration
	timer := time.NewTimer(tm.MaxDuration)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Timer expired - stop container if auto-stop is enabled
		if tm.AutoStop {
			tm.handleTimeout()
		} else {
			tm.Logger.Printf("[limits] Runtime limit reached (%s) but auto_stop is disabled", tm.MaxDuration)
		}
	case <-tm.ctx.Done():
		// Monitor was cancelled before timeout
		return
	}
}

// handleTimeout handles the timeout event by stopping the container
func (tm *TimeoutMonitor) handleTimeout() {
	stopType := "gracefully"
	if !tm.StopGraceful {
		stopType = "forcefully"
	}
	tm.Logger.Printf("[limits] Runtime limit reached (%s), stopping container %s...", tm.MaxDuration, stopType)

	mgr := container.NewManager(tm.ContainerName)

	if tm.StopGraceful {
		// Graceful: try non-force first, escalate to force if needed
		// StopGraceful=true means graceful shutdown (force=false)
		if err := mgr.Stop(false); err != nil {
			tm.Logger.Errorf("[limits] Graceful stop failed: %v, forcing...", err)
			_ = mgr.Stop(true)
		}

		// Verify container actually stopped, force if still running
		time.Sleep(5 * time.Second)
		if running, _ := mgr.Running(); running {
			tm.Logger.Println("[limits] Container still running after graceful stop, forcing...")
			_ = mgr.Stop(true)
		}
	} else {
		// StopGraceful=false means force stop immediately (force=true)
		if err := mgr.Stop(true); err != nil {
			tm.Logger.Errorf("[limits] Error force-stopping container: %v", err)
			return
		}
	}

	tm.Logger.Println("[limits] Container stopped due to runtime limit")
}

// Stop stops the timeout monitor
// This should be called when the session ends normally (before timeout)
func (tm *TimeoutMonitor) Stop() {
	tm.cancel()
	// Wait for the background goroutine to finish
	<-tm.done
}
