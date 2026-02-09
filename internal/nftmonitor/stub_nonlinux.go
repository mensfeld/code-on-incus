//go:build !linux
// +build !linux

package nftmonitor

import (
	"context"
	"fmt"
)

// Daemon stub for non-Linux platforms
type Daemon struct{}

// StartDaemon returns an error on non-Linux platforms
func StartDaemon(ctx context.Context, cfg Config) (*Daemon, error) {
	return nil, fmt.Errorf("nftables monitoring is only supported on Linux")
}

// Stop is a no-op on non-Linux platforms
func (d *Daemon) Stop() error {
	return nil
}

// IsRunning always returns false on non-Linux platforms
func (d *Daemon) IsRunning() bool {
	return false
}
