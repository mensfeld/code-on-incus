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
