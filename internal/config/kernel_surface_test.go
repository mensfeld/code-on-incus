package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// Defaults: docker on, hardening off — the pre-flag behavior.
func TestKernelSurface_Defaults(t *testing.T) {
	cfg := GetDefaultConfig()
	if !cfg.Container.IsDockerEnabled() {
		t.Error("docker should default to enabled")
	}
	if cfg.Security.IsReduceKernelSurfaceEnabled() {
		t.Error("reduce_kernel_surface should default to disabled")
	}
}

func TestKernelSurface_TOMLParse(t *testing.T) {
	const tomlSrc = `
[container]
docker = false

[security]
reduce_kernel_surface = true
`
	var cfg Config
	if _, err := toml.Decode(tomlSrc, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Container.IsDockerEnabled() {
		t.Error("docker=false not parsed")
	}
	if !cfg.Security.IsReduceKernelSurfaceEnabled() {
		t.Error("reduce_kernel_surface=true not parsed")
	}
}

// The strict tier parses, and it IMPLIES the base tier: setting only
// reduce_kernel_surface_strict makes IsReduceKernelSurfaceEnabled true so the
// base deny list + docker-off still apply, plus the strict extra.
func TestKernelSurface_StrictImpliesBase(t *testing.T) {
	const tomlSrc = `
[security]
reduce_kernel_surface_strict = true
`
	var cfg Config
	if _, err := toml.Decode(tomlSrc, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !cfg.Security.IsReduceKernelSurfaceStrictEnabled() {
		t.Error("reduce_kernel_surface_strict=true not parsed")
	}
	if !cfg.Security.IsReduceKernelSurfaceEnabled() {
		t.Error("strict tier must imply the base tier (IsReduceKernelSurfaceEnabled)")
	}
}

// IsReduceKernelSurfaceBaseEnabled distinguishes the base flag the user
// actually wrote from the strict tier's implication — so messaging can name the
// right flag. Strict-only must NOT report the base flag as set.
func TestKernelSurface_BaseVsStrictDistinction(t *testing.T) {
	yes := true

	strictOnly := &SecurityConfig{ReduceKernelSurfaceStrict: &yes}
	if strictOnly.IsReduceKernelSurfaceBaseEnabled() {
		t.Error("strict-only config must not report the base flag as explicitly set")
	}
	if !strictOnly.IsReduceKernelSurfaceEnabled() {
		t.Error("strict-only must still count as hardening-enabled (implies base)")
	}

	baseOnly := &SecurityConfig{ReduceKernelSurface: &yes}
	if !baseOnly.IsReduceKernelSurfaceBaseEnabled() {
		t.Error("base flag set must report base-enabled")
	}
}

// Strict tier defaults to off, and does not turn on just because the base flag is set.
func TestKernelSurface_StrictDefaultsOff(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.Security.IsReduceKernelSurfaceStrictEnabled() {
		t.Error("reduce_kernel_surface_strict should default to disabled")
	}
	yes := true
	cfg.Security.ReduceKernelSurface = &yes
	if cfg.Security.IsReduceKernelSurfaceStrictEnabled() {
		t.Error("base flag must not imply the strict tier")
	}
}

// The strict flag is trusted-scope-only in both directions, exactly like the base flag.
func TestKernelSurface_StrictUntrustedStripped(t *testing.T) {
	yes, no := true, false

	cfg := &Config{}
	cfg.Security.ReduceKernelSurfaceStrict = &no
	sanitizeUntrustedConfig(cfg, "/ws/.coi/config.toml")
	if cfg.Security.ReduceKernelSurfaceStrict != nil {
		t.Error("untrusted reduce_kernel_surface_strict=false must be stripped")
	}

	cfg = &Config{}
	cfg.Security.ReduceKernelSurfaceStrict = &yes
	sanitizeUntrustedConfig(cfg, "/ws/.coi/config.toml")
	if cfg.Security.ReduceKernelSurfaceStrict != nil {
		t.Error("untrusted reduce_kernel_surface_strict=true must be stripped (trusted scope only)")
	}
}

// reduce_kernel_surface wins over an explicit docker=true. The raw flags are
// kept as set (that's the config layer's job); the precedence is resolved by
// container.HardeningPolicy.DockerEnabled, mirrored inline here since the config
// package cannot import container. The App/session layers build the policy from
// exactly these two accessors.
func TestKernelSurface_HardeningWinsOverDocker(t *testing.T) {
	yes := true
	cfg := GetDefaultConfig()
	cfg.Container.Docker = &yes
	cfg.Security.ReduceKernelSurface = &yes
	effectiveDocker := cfg.Container.IsDockerEnabled() && !cfg.Security.IsReduceKernelSurfaceEnabled()
	if effectiveDocker {
		t.Error("reduce_kernel_surface=true must disable docker even when docker=true is explicit")
	}
}

// Later scopes override earlier ones via pointer-merge for both flags.
func TestKernelSurface_MergePrecedence(t *testing.T) {
	yes, no := true, false
	base := GetDefaultConfig()
	overlay := &Config{}
	overlay.Container.Docker = &no
	overlay.Security.ReduceKernelSurface = &yes
	base.Merge(overlay)
	if base.Container.IsDockerEnabled() {
		t.Error("overlay docker=false should win over unset base")
	}
	if !base.Security.IsReduceKernelSurfaceEnabled() {
		t.Error("overlay reduce_kernel_surface=true should win over unset base")
	}
	// And a further overlay flipping docker back on wins again.
	overlay2 := &Config{}
	overlay2.Container.Docker = &yes
	base.Merge(overlay2)
	if !base.Container.IsDockerEnabled() {
		t.Error("later overlay docker=true should win")
	}
}

// Untrusted repo config: docker=true (re-widening) is stripped, docker=false
// (tightening) survives, reduce_kernel_surface is stripped in both directions.
func TestSanitizeUntrustedConfig_KernelSurface(t *testing.T) {
	yes, no := true, false

	cfg := &Config{}
	cfg.Container.Docker = &yes
	cfg.Security.ReduceKernelSurface = &no
	sanitizeUntrustedConfig(cfg, "/ws/.coi/config.toml")
	if cfg.Container.Docker != nil {
		t.Error("untrusted docker=true must be stripped")
	}
	if cfg.Security.ReduceKernelSurface != nil {
		t.Error("untrusted reduce_kernel_surface=false must be stripped")
	}

	cfg = &Config{}
	cfg.Container.Docker = &no
	cfg.Security.ReduceKernelSurface = &yes
	sanitizeUntrustedConfig(cfg, "/ws/.coi/config.toml")
	if cfg.Container.Docker == nil || *cfg.Container.Docker {
		t.Error("untrusted docker=false (tightening) must survive")
	}
	if cfg.Security.ReduceKernelSurface != nil {
		t.Error("untrusted reduce_kernel_surface=true must be stripped (trusted scope only)")
	}
}
