package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	coischema "github.com/mensfeld/code-on-incus/schema"
)

// Load loads configuration from all available sources
// Hierarchy (lowest to highest precedence):
// 1. Built-in defaults
// 2. User config (~/.coi/config.toml, or the file $COI_CONFIG points at)
// 3. Project config (./.coi/config.toml)
//
// There are no env-var config overrides: config-shaped settings live in
// config files and profiles only.
//
// Profile directories are scanned independently from config files.
// See GetProfileParentDirs() for the full list of scanned locations.
// Profiles from all scan locations are merged into a single namespace; if the
// same profile name is defined in more than one location Load() returns an
// error asking the user to resolve the conflict.
func Load() (*Config, error) {
	// Check for deprecated .coi.toml in project root
	if err := checkDeprecatedConfig(); err != nil {
		return nil, err
	}

	// Start with defaults
	cfg := GetDefaultConfig()

	// Scan profile directories from all known locations. Profiles are merged
	// into a single namespace; duplicate names across locations are rejected
	// by loadProfileDirectories so it's always clear which profile is used.
	for _, dir := range GetProfileParentDirs() {
		if err := loadProfileDirectories(cfg, dir, isTrustedProfileDir(dir)); err != nil {
			return nil, err
		}
	}

	// Load from config files (in order). The user config (~/.coi/config.toml) and
	// an explicit COI_CONFIG are trusted; the project config (<cwd>/.coi/config.toml)
	// is untrusted and sanitized before merging.
	paths := GetConfigPaths()
	for _, path := range paths {
		if err := loadConfigFileScoped(cfg, path, isTrustedConfigPath(path)); err != nil {
			// Only return error if file exists but can't be parsed
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load config from %s: %w", path, err)
			}
			// File doesn't exist - that's OK, skip it
		}
	}

	// Inject built-in "default" profile (can be overridden by disk-based profiles)
	if _, exists := cfg.Profiles["default"]; !exists {
		cfg.Profiles["default"] = synthesizeDefaultProfile(cfg)
	}

	// Inject built-in "hardened" profile: a hardened preset for opening untrusted
	// repos (restricted network, secret masking, immutability, ephemeral,
	// no SSH-agent forwarding, monitoring). Overridable by a disk profile.
	if _, exists := cfg.Profiles["hardened"]; !exists {
		cfg.Profiles["hardened"] = synthesizeHardenedProfile()
	}

	// Resolve profile inheritance after all profiles are loaded from all levels
	if err := cfg.ResolveProfileInheritance(); err != nil {
		return nil, fmt.Errorf("profile inheritance error: %w", err)
	}

	// Ensure directories exist
	if err := ensureDirectories(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// OverlayProjectConfig loads the project config from the given workspace directory
// and merges it into cfg. This is used when launching via alias from a different
// directory, so the resolved workspace's .coi/config.toml is applied.
//
// The workspace .coi/config.toml is always project-scoped and therefore
// untrusted (it may be supplied by a cloned repo or planted by an in-container
// agent), so it is loaded with trusted=false and sanitized.
func (c *Config) OverlayProjectConfig(workspaceDir string) error {
	path := filepath.Join(workspaceDir, ".coi", "config.toml")
	return loadConfigFileScoped(c, path, false)
}

// isTrustedConfigPath reports whether a config path is user-controlled and
// therefore trusted: the user's ~/.coi/config.toml or an explicit COI_CONFIG.
// The project config (<cwd>/.coi/config.toml) is untrusted.
func isTrustedConfigPath(path string) bool {
	clean := filepath.Clean(path)
	if home, err := os.UserHomeDir(); err == nil {
		if clean == filepath.Clean(filepath.Join(home, ".coi", "config.toml")) {
			return true
		}
	}
	if env := os.Getenv("COI_CONFIG"); env != "" && clean == filepath.Clean(env) {
		return true
	}
	return false
}

// loadConfigFileScoped loads a TOML config file and merges it into cfg.
//
// trusted distinguishes config the user explicitly controls (~/.coi/config.toml
// and an explicit COI_CONFIG) from untrusted, project-scoped config (the
// workspace's .coi/config.toml). Untrusted config is sanitized before merging:
// any network setting that would WEAKEN isolation is dropped with a warning.
// This prevents a malicious repo — or an in-container agent that planted a
// .coi/config.toml — from silently turning off the private-network / metadata
// blocks on the next launch.
func loadConfigFileScoped(cfg *Config, path string, trusted bool) error {
	// Check if file exists
	if _, err := os.Stat(path); err != nil {
		return err
	}

	// Parse TOML file
	var fileCfg Config
	if _, err := toml.DecodeFile(path, &fileCfg); err != nil {
		return err
	}

	// Detect pre-0.8.0 layouts and refuse to load with an actionable error.
	if err := checkDeprecatedConfigFields(path); err != nil {
		return err
	}

	if !trusted {
		sanitizeUntrustedConfig(&fileCfg, path)
		markUntrustedMounts(fileCfg.Mounts.Default, path)
		markUntrustedSockets(fileCfg.Sockets, path)
	}

	configDir := filepath.Dir(path)

	// Resolve build script path relative to config file location
	if fileCfg.Container.Build.Script != "" {
		fileCfg.Container.Build.Script = resolveRelativePath(configDir, fileCfg.Container.Build.Script)
	}

	// Merge into main config
	cfg.Merge(&fileCfg)

	return nil
}

// sanitizeUntrustedConfig hardens a project-scoped (untrusted) config before it
// is merged. It refuses network settings that would weaken isolation relative to
// the secure defaults, leaving the stronger built-in/user values in place.
// Strengthening values are left intact.
func sanitizeUntrustedConfig(fileCfg *Config, path string) {
	sanitizeUntrustedNetwork(&fileCfg.Network, path)
	sanitizeUntrustedEnvCommands(&fileCfg.Defaults, path)
	sanitizeUntrustedSecurity(&fileCfg.Security, path)
	sanitizeUntrustedGit(&fileCfg.Git, path)

	// Persistence is honored from project scope (not a protection downgrade —
	// the container stays fully sandboxed), but a cloned repo opting the user
	// into container reuse deserves a visible note: state from a previous
	// run's container (which executed repo-controlled code) is carried into
	// subsequent runs.
	if fileCfg.Container.Persistent != nil && *fileCfg.Container.Persistent {
		fmt.Fprintf(os.Stderr,
			"Note: project config %s enables persistent containers — container state is reused across runs\n", path)
	}
}

// warnUntrustedDowngrade reports that a protection-weakening field from an
// untrusted (project-scoped) source was ignored.
func warnUntrustedDowngrade(path, field string) {
	fmt.Fprintf(os.Stderr,
		"WARNING: ignoring '%s' in project config %s; removing read-only "+
			"protection is a security downgrade. Move it to ~/.coi/config.toml or "+
			"set COI_CONFIG to apply it.\n", field, path)
}

// sanitizeUntrustedSecurity drops security-weakening fields from an untrusted
// source. A project config (or project-scoped profile) may *add* protections (via
// additional_protected_paths) but must never *remove* them, because honoring a
// downgrade from a cloned/agent-planted repo would let it turn off read-only
// protection of host-auto-executing files (e.g. .git/hooks, .claude/settings.json).
// Every field that can shrink the effective protected set is stripped so it is
// honored only from trusted-scope config (~/.coi/config.toml or $COI_CONFIG):
//   - disable_protection (removes ALL protections),
//   - protected_paths (a full replace that drops the defaults; untrusted sources
//     can still extend via additional_protected_paths),
//   - writable_paths (subtracts entries),
//   - host_immutable=false (disables the chattr +i hardening).
//
// nil is a no-op.
func sanitizeUntrustedSecurity(s *SecurityConfig, path string) {
	if s == nil {
		return
	}
	if s.DisableProtection {
		warnUntrustedDowngrade(path, "security.disable_protection")
		s.DisableProtection = false
	}
	if len(s.ProtectedPaths) > 0 {
		warnUntrustedDowngrade(path, "security.protected_paths")
		s.ProtectedPaths = nil
	}
	if len(s.WritablePaths) > 0 {
		warnUntrustedDowngrade(path, "security.writable_paths")
		s.WritablePaths = nil
	}
	if s.HostImmutable != nil && !*s.HostImmutable {
		// Only the disabling value is a downgrade; a strengthening true is dropped
		// too for simplicity (trusted config controls host-immutable).
		warnUntrustedDowngrade(path, "security.host_immutable")
	}
	s.HostImmutable = nil
}

// sanitizeUntrustedGit drops git settings that weaken protection from an
// untrusted source. git.writable_hooks=true makes .git/hooks writable inside the
// container (filterWritableGitHooks removes it from the read-only set), so it is
// honored only from trusted-scope config. nil is a no-op.
func sanitizeUntrustedGit(g *GitConfig, path string) {
	if g == nil || g.WritableHooks == nil {
		return
	}
	if *g.WritableHooks {
		warnUntrustedDowngrade(path, "git.writable_hooks")
	}
	g.WritableHooks = nil
}

// sanitizeUntrustedEnvCommands strips env_commands (and their timeout) from an
// untrusted source. Running a host command at session start is host code
// execution, so — unlike escaping mounts/sockets, which can be approved via
// `coi trust` — it is never honored from a project config or project-scoped
// profile; it must live in trusted-scope config (~/.coi/config.toml / $COI_CONFIG).
func sanitizeUntrustedEnvCommands(d *DefaultsConfig, path string) {
	if d == nil || len(d.EnvCommands) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr,
		"WARNING: ignoring 'env_commands' in project config %s; running a host "+
			"command is host code execution. Move it to ~/.coi/config.toml or set "+
			"COI_CONFIG to apply it.\n", path)
	d.EnvCommands = nil
	d.EnvCommandTimeout = ""
}

// sanitizeUntrustedNetwork drops security-downgrading network settings from an
// untrusted source (a project config or a project-scoped profile). nil is a
// no-op so it can be called directly on a profile's optional *NetworkConfig.
func sanitizeUntrustedNetwork(n *NetworkConfig, path string) {
	if n == nil {
		return
	}
	refuse := func(what string) {
		fmt.Fprintf(os.Stderr,
			"WARNING: ignoring security-downgrading %q in project config %s; "+
				"move it to ~/.coi/config.toml or set COI_CONFIG to apply it.\n", what, path)
	}
	if n.BlockPrivateNetworks != nil && !*n.BlockPrivateNetworks {
		refuse("network.block_private_networks=false")
		n.BlockPrivateNetworks = nil
	}
	if n.BlockMetadataEndpoint != nil && !*n.BlockMetadataEndpoint {
		refuse("network.block_metadata_endpoint=false")
		n.BlockMetadataEndpoint = nil
	}
	if n.AllowLocalNetworkAccess != nil && *n.AllowLocalNetworkAccess {
		refuse("network.allow_local_network_access=true")
		n.AllowLocalNetworkAccess = nil
	}
	if n.Mode == NetworkModeOpen {
		refuse("network.mode=open")
		n.Mode = ""
	}
}

// markUntrustedMounts tags mounts from an untrusted source so escaping host
// mounts are gated behind explicit trust at apply time (internal/session/trust.go).
// SourcePath is the absolute config path; if Abs fails the raw path is used —
// the mounts are still marked untrusted so a path-resolution error cannot fail
// open and admit an ungated host mount.
func markUntrustedMounts(mounts []MountEntry, path string) {
	src := path
	if abs, err := filepath.Abs(path); err == nil {
		src = abs
	}
	for i := range mounts {
		mounts[i].Untrusted = true
		mounts[i].SourcePath = src
	}
}

// markUntrustedSockets tags forwarded sockets from an untrusted source so they
// are gated behind explicit trust at forward time (internal/session/trust.go).
// Like markUntrustedMounts, a path-resolution error falls back to the raw path
// rather than failing open and forwarding an ungated host socket.
func markUntrustedSockets(sockets []SocketEntry, path string) {
	src := path
	if abs, err := filepath.Abs(path); err == nil {
		src = abs
	}
	for i := range sockets {
		sockets[i].Untrusted = true
		sockets[i].SourcePath = src
	}
}

// isTrustedProfileDir reports whether a profiles parent directory is
// user-controlled (trusted): the user's ~/.coi or the directory of an explicit
// $COI_CONFIG. The project workspace's ./.coi is untrusted — a cloned repo can
// ship profiles under it.
func isTrustedProfileDir(dir string) bool {
	clean := filepath.Clean(dir)
	if home, err := os.UserHomeDir(); err == nil {
		if clean == filepath.Clean(filepath.Join(home, ".coi")) {
			return true
		}
	}
	if env := os.Getenv("COI_CONFIG"); env != "" && clean == filepath.Clean(filepath.Dir(env)) {
		return true
	}
	return false
}

// loadProfileDirectories scans configDir/profiles/ for subdirectories containing config.toml
// and adds them to cfg.Profiles. Each subdirectory name becomes the profile name.
//
// If a profile with the same name is already loaded from a different source
// location, loadProfileDirectories returns an error. Profiles from different
// scan locations (e.g. ~/.coi/profiles/ and ./.coi/profiles/) are merged
// together, and users are expected to use unique names across locations so
// it's always clear which profile is being used.
func loadProfileDirectories(cfg *Config, configDir string, trusted bool) error {
	profilesDir := filepath.Join(configDir, "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil // Directory doesn't exist or can't be read — that's fine
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		profileName := entry.Name()
		profileConfigPath := filepath.Join(profilesDir, profileName, "config.toml")

		if _, err := os.Stat(profileConfigPath); err != nil {
			continue // No config.toml in this subdirectory
		}

		// Detect duplicate profile name across scan locations. The same path
		// loaded twice is fine (idempotent), but two different paths for the
		// same name is ambiguous — error out and tell the user to rename one.
		if existing, ok := cfg.Profiles[profileName]; ok && existing.Source != "" && existing.Source != profileConfigPath {
			return fmt.Errorf(
				"profile %q defined in multiple locations:\n  %s\n  %s\n"+
					"Rename one of them or delete the duplicate so it's clear which profile is being used",
				profileName, existing.Source, profileConfigPath)
		}

		var profileCfg ProfileConfig
		if _, err := toml.DecodeFile(profileConfigPath, &profileCfg); err != nil {
			return fmt.Errorf("failed to parse profile %q config at %s: %w", profileName, profileConfigPath, err)
		}

		// Detect pre-0.8.0 profile layouts and refuse to load with a friendly
		// message before schema validation runs (deprecated fields would
		// otherwise surface as unhelpful "additionalProperties not allowed").
		if err := checkDeprecatedProfileFields(profileConfigPath); err != nil {
			return err
		}

		// Validate the raw TOML against the JSON Schema to catch unknown keys,
		// wrong types, and invalid enum values with precise error paths.
		var rawMap map[string]any
		if _, err := toml.DecodeFile(profileConfigPath, &rawMap); err != nil {
			return fmt.Errorf("failed to re-read profile %q for schema validation: %w", profileName, err)
		}
		if schemaErr := coischema.ValidateProfileMap(rawMap); schemaErr != nil {
			return fmt.Errorf("profile %q at %s failed schema validation: %w",
				profileName, profileConfigPath, schemaErr)
		}

		// Resolve paths relative to profile directory
		profileDir := filepath.Join(profilesDir, profileName)
		if profileCfg.Container.Build.Script != "" {
			profileCfg.Container.Build.Script = resolveRelativePath(profileDir, profileCfg.Container.Build.Script)
		}
		if profileCfg.Context != "" {
			profileCfg.Context = resolveRelativePath(profileDir, profileCfg.Context)
		}

		// Tag with source location
		profileCfg.Source = profileConfigPath

		// A project-scoped profile (under the workspace ./.coi) is untrusted — a
		// cloned repo can ship it. Apply the same hardening as an untrusted
		// top-level config: refuse network downgrades and tag its mounts so
		// escaping host mounts are gated behind `coi trust`.
		if !trusted {
			sanitizeUntrustedNetwork(profileCfg.Network, profileConfigPath)
			sanitizeUntrustedSecurity(profileCfg.Security, profileConfigPath)
			sanitizeUntrustedGit(profileCfg.Git, profileConfigPath)
			markUntrustedMounts(profileCfg.Mounts, profileConfigPath)
			markUntrustedSockets(profileCfg.Sockets, profileConfigPath)
			if len(profileCfg.EnvCommands) > 0 {
				fmt.Fprintf(os.Stderr,
					"WARNING: ignoring 'env_commands' in project profile %s; running a "+
						"host command is host code execution. Move it to a profile under "+
						"~/.coi/profiles to apply it.\n", profileConfigPath)
				profileCfg.EnvCommands = nil
			}
		}

		if cfg.Profiles == nil {
			cfg.Profiles = make(map[string]ProfileConfig)
		}
		cfg.Profiles[profileName] = profileCfg
	}
	return nil
}

// ensureDirectories creates necessary directories if they don't exist
func ensureDirectories(cfg *Config) error {
	dirs := []string{
		cfg.Paths.SessionsDir,
		cfg.Paths.StorageDir,
		cfg.Paths.LogsDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// checkDeprecatedConfig checks for the deprecated .coi.toml file in the current directory.
// Returns an error with migration instructions if found.
func checkDeprecatedConfig() error {
	workDir, err := os.Getwd()
	if err != nil {
		return nil // Can't check, skip
	}

	oldPath := filepath.Join(workDir, ".coi.toml")
	if _, err := os.Stat(oldPath); err == nil {
		return fmt.Errorf(
			"found .coi.toml in project root. As of this version, project config must be placed in .coi/config.toml. " +
				"Please move your config: mkdir -p .coi && mv .coi.toml .coi/config.toml",
		)
	}

	return nil
}

// resolveRelativePath resolves a path relative to a base directory.
// Absolute paths and ~-prefixed paths are returned as-is (with ~ expanded).
func resolveRelativePath(baseDir, path string) string {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return ExpandPath(path)
	}
	return filepath.Join(baseDir, path)
}
