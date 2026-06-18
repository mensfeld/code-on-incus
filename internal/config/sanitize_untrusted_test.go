package config

import "testing"

// Untrusted (project-scoped) config must have any security-WEAKENING network
// setting dropped.
func TestSanitizeUntrustedConfig_DropsDowngrades(t *testing.T) {
	no, yes := false, true
	cfg := &Config{}
	cfg.Network.BlockPrivateNetworks = &no
	cfg.Network.BlockMetadataEndpoint = &no
	cfg.Network.AllowLocalNetworkAccess = &yes
	cfg.Network.Mode = NetworkModeOpen

	sanitizeUntrustedConfig(cfg, "/ws/.coi/config.toml")

	if cfg.Network.BlockPrivateNetworks != nil {
		t.Error("block_private_networks=false should be dropped")
	}
	if cfg.Network.BlockMetadataEndpoint != nil {
		t.Error("block_metadata_endpoint=false should be dropped")
	}
	if cfg.Network.AllowLocalNetworkAccess != nil {
		t.Error("allow_local_network_access=true should be dropped")
	}
	if cfg.Network.Mode == NetworkModeOpen {
		t.Error("mode=open should be dropped")
	}
}

// Strengthening / neutral network settings from untrusted config must be kept.
func TestSanitizeUntrustedConfig_KeepsStrengthening(t *testing.T) {
	yes := true
	cfg := &Config{}
	cfg.Network.BlockPrivateNetworks = &yes // strengthening
	cfg.Network.BlockMetadataEndpoint = &yes
	cfg.Network.Mode = NetworkModeRestricted // not a downgrade

	sanitizeUntrustedConfig(cfg, "/ws/.coi/config.toml")

	if cfg.Network.BlockPrivateNetworks == nil || !*cfg.Network.BlockPrivateNetworks {
		t.Error("block_private_networks=true (strengthening) should be kept")
	}
	if cfg.Network.BlockMetadataEndpoint == nil || !*cfg.Network.BlockMetadataEndpoint {
		t.Error("block_metadata_endpoint=true (strengthening) should be kept")
	}
	if cfg.Network.Mode != NetworkModeRestricted {
		t.Error("mode=restricted (not a downgrade) should be kept")
	}
}

// An untrusted (project-scoped) config must not be able to remove read-only
// protections: writable_paths is a security downgrade and must be dropped so a
// cloned repo cannot turn off protection of host-auto-executing files.
func TestSanitizeUntrustedConfig_DropsWritablePaths(t *testing.T) {
	cfg := &Config{}
	cfg.Security.WritablePaths = []string{".claude/settings.json", ".git/hooks"}

	sanitizeUntrustedConfig(cfg, "/ws/.coi/config.toml")

	if cfg.Security.WritablePaths != nil {
		t.Error("security.writable_paths from untrusted config should be dropped")
	}
}

// writable_paths from a trusted-scope config is honored (no sanitization runs).
func TestTrustedConfig_KeepsWritablePaths(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Merge(&Config{Security: SecurityConfig{WritablePaths: []string{".claude/settings.json"}}})

	for _, p := range cfg.Security.GetEffectiveProtectedPaths() {
		if p == ".claude/settings.json" {
			t.Error("trusted writable_paths should make .claude/settings.json writable")
		}
	}
}

// A trusted config path must be recognized; a project path must not.
func TestIsTrustedConfigPath(t *testing.T) {
	t.Setenv("COI_CONFIG", "/explicit/coi.toml")
	if !isTrustedConfigPath("/explicit/coi.toml") {
		t.Error("explicit COI_CONFIG path should be trusted")
	}
	if isTrustedConfigPath("/some/project/.coi/config.toml") {
		t.Error("project .coi/config.toml should NOT be trusted")
	}
}
