package limits

import (
	"context"
	"fmt"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// TimeoutMonitor monitors a container's runtime and stops it when max duration is reached
type TimeoutMonitor struct {
	ContainerName string
	MaxDuration   time.Duration
	AutoStop      bool
	StopGraceful  bool
	Project       string
	Logger        func(string)

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// NewTimeoutMonitor creates a new timeout monitor
func NewTimeoutMonitor(containerName string, maxDuration time.Duration, autoStop, stopGraceful bool, project string, logger func(string)) *TimeoutMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &TimeoutMonitor{
		ContainerName: containerName,
		MaxDuration:   maxDuration,
		AutoStop:      autoStop,
		StopGraceful:  stopGraceful,
		Project:       project,
		Logger:        logger,
		ctx:           ctx,
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

	if tm.Logger != nil {
		tm.Logger(fmt.Sprintf("[limits] Container will auto-stop after %s", tm.MaxDuration))
	}

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
			if tm.Logger != nil {
				tm.Logger(fmt.Sprintf("[limits] Runtime limit reached (%s) but auto_stop is disabled", tm.MaxDuration))
			}
		}
	case <-tm.ctx.Done():
		// Monitor was cancelled before timeout
		return
	}
}

// handleTimeout handles the timeout event by stopping the container
func (tm *TimeoutMonitor) handleTimeout() {
	if tm.Logger != nil {
		stopType := "gracefully"
		if !tm.StopGraceful {
			stopType = "forcefully"
		}
		tm.Logger(fmt.Sprintf("[limits] Runtime limit reached (%s), stopping container %s...", tm.MaxDuration, stopType))
	}

	// Create a container manager
	mgr := container.NewManager(tm.ContainerName)

	// Stop the container
	if err := mgr.Stop(tm.StopGraceful); err != nil {
		if tm.Logger != nil {
			tm.Logger(fmt.Sprintf("[limits] Error stopping container: %v", err))
		}
		return
	}

	if tm.Logger != nil {
		tm.Logger("[limits] Container stopped due to runtime limit")
	}
}

// Stop stops the timeout monitor
// This should be called when the session ends normally (before timeout)
func (tm *TimeoutMonitor) Stop() {
	tm.cancel()
	// Wait for the background goroutine to finish
	<-tm.done
}

// Wait blocks until the monitor completes (either timeout or cancelled)
func (tm *TimeoutMonitor) Wait() {
	<-tm.done
}

// Remaining returns the remaining time until timeout
// Returns 0 if the monitor has already stopped or timed out
func (tm *TimeoutMonitor) Remaining() time.Duration {
	select {
	case <-tm.done:
		return 0
	default:
		// This is approximate - we don't track start time precisely
		// For a more accurate implementation, we'd need to store start time
		return tm.MaxDuration
	}
}
