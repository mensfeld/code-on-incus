package network

import (
	"context"
	"fmt"
	"log"

	"github.com/mensfeld/claude-on-incus/internal/config"
)

// Manager provides high-level network isolation management for containers
type Manager struct {
	config        *config.NetworkConfig
	acl           *ACLManager
	containerName string
	aclName       string
}

// NewManager creates a new network manager with the specified configuration
func NewManager(cfg *config.NetworkConfig) *Manager {
	return &Manager{
		config: cfg,
		acl:    &ACLManager{},
	}
}

// SetupForContainer configures network isolation for a container
func (m *Manager) SetupForContainer(ctx context.Context, containerName string) error {
	m.containerName = containerName

	// In open mode, no ACL needed - use default network behavior
	if m.config.Mode == config.NetworkModeOpen {
		log.Println("Network mode: open (no restrictions)")
		return nil
	}

	// In restricted mode, create and apply ACL
	log.Println("Network mode: restricted (blocking local/internal networks)")

	// Generate ACL name
	m.aclName = fmt.Sprintf("coi-%s-restricted", containerName)

	// 1. Create ACL with block rules
	if err := m.acl.Create(m.aclName, m.config); err != nil {
		return fmt.Errorf("failed to create network ACL: %w", err)
	}

	// 2. Apply ACL to container
	if err := m.acl.ApplyToContainer(containerName, m.aclName); err != nil {
		// If applying fails, clean up the ACL
		_ = m.acl.Delete(m.aclName)
		return fmt.Errorf("failed to apply network ACL: %w", err)
	}

	log.Printf("Network ACL '%s' applied successfully", m.aclName)

	// Log what is blocked
	if m.config.BlockPrivateNetworks {
		log.Println("  ✓ Blocking private networks (RFC1918)")
	}
	if m.config.BlockMetadataEndpoint {
		log.Println("  ✓ Blocking cloud metadata endpoints")
	}

	return nil
}

// Teardown removes network isolation for a container
func (m *Manager) Teardown(ctx context.Context, containerName string) error {
	// Nothing to clean up in open mode
	if m.config.Mode == config.NetworkModeOpen {
		return nil
	}

	// Remove ACL
	aclName := m.aclName
	if aclName == "" {
		// Fallback if aclName wasn't set
		aclName = fmt.Sprintf("coi-%s-restricted", containerName)
	}

	if err := m.acl.Delete(aclName); err != nil {
		// Don't fail teardown if ACL deletion fails
		// The ACL might have been already removed or never created
		log.Printf("Warning: failed to delete network ACL '%s': %v", aclName, err)
		return nil
	}

	log.Printf("Network ACL '%s' removed", aclName)
	return nil
}

// GetMode returns the current network mode
func (m *Manager) GetMode() config.NetworkMode {
	return m.config.Mode
}
