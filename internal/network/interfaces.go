package network

import "context"

// NetworkManager is the interface implemented by *Manager.
// Defining it here enables session and cli packages to accept a stub
// in tests without requiring nftables or a live container network.
type NetworkManager interface {
	SetupForContainer(ctx context.Context, containerName string) error
	Teardown(ctx context.Context, containerName string) error
}

// Compile-time assertion: *Manager must satisfy NetworkManager.
var _ NetworkManager = (*Manager)(nil)
