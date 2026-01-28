# CHANGELOG

## 0.6.0 (Unreleased)

### Bug Fixes

- [Bug Fix] **Network ACL support detection** - Fixed `coi shell` failing with "Invalid device option 'security.acls'" error on standard Incus bridge networks. The tool now detects whether the network supports ACLs (requires OVN) and provides a clear error message with remediation steps instead of a cryptic failure. Users on standard bridge networks are guided to either use `--network=open` or configure OVN networking. (#63)
- [Bug Fix] **`coi list --all` always shows Saved Sessions section** - Fixed bug where "Saved Sessions:" section would not appear when using `--all` flag if no sessions with saved state existed. The function `listSavedSessions()` was returning `nil` instead of an empty slice, causing the section to be skipped entirely. Now properly initializes as empty slice so the section always appears with `--all`, showing "(none)" when empty. This makes the output predictable and consistent. (#81)
- [Bug Fix] **Tool-agnostic session listing** - Fixed `coi list --all` hardcoding `.claude` directory check, which broke support for other AI coding tools (Aider, Cursor, etc.). Now uses `tool.ConfigDirName()` to dynamically check for the configured tool's config directory (e.g., `.aider/`, `.cursor/`). Also handles ENV-based tools (no config directory) by only checking for `metadata.json`. This ensures saved sessions are properly detected regardless of which AI tool is configured. (#81)
- [Bug Fix] **Test isolation improvements** - Fixed `test_list_format_json_empty` and `test_attach_shows_sessions` failing intermittently due to containers left by previous tests in random test order. Added `cleanup_containers` fixture and explicit `kill --all --force` before test execution to ensure clean state. This prevents test interference from pytest-randomly's randomized execution order. (#81)
- [Bug Fix] **macOS Colima build timeout handling** - Fixed macOS Colima installation tests hanging for 45 minutes when `coi build` gets stuck during containerd/Docker setup. Added 15-minute timeout per build attempt with automatic retry on timeout (exit code 124) in addition to existing network failure retry logic. Prevents CI jobs from hanging until job timeout - now retries after 15 minutes for transient Colima VM issues. (#81)

### Features

- [Feature] **Automatic OVN host routing** - COI now automatically configures host routes to OVN networks, allowing you to access container services (web servers, databases, APIs) directly from your host machine. When starting a container on an OVN network, COI detects the network type, extracts the OVN subnet and uplink IP, and adds the necessary routing rule. If sudo access is unavailable, COI displays the exact command needed: `sudo ip route add <subnet> via <uplink-ip> dev <bridge>`. This eliminates the "Connection refused" or "No route to host" errors when trying to curl/access services running in containers with network isolation (restricted/allowlist modes). Works automatically with all OVN-based networks - no configuration required. (#81)
- [Feature] **Automatic Docker/nested container support** - COI now automatically enables Docker and container nesting support on all containers by setting `security.nesting=true`, `security.syscalls.intercept.mknod=true`, and `security.syscalls.intercept.setxattr=true`. This eliminates the "unable to start container process: error during container init: open sysctl net.ipv4.ip_unprivileged_port_start file: reopen fd 8: permission denied" error when running Docker inside Incus containers. No configuration required - Docker just works out of the box.
- [Feature] **Automatic Colima/Lima environment detection** - COI now automatically detects when running inside a Colima or Lima VM and disables UID shifting. These VMs already handle UID mapping at the VM level via virtiofs, making Incus's `shift=true` unnecessary and problematic. Detection checks for virtiofs mounts in `/proc/mounts` and the `lima` user. Users no longer need to manually configure `disable_shift` option.
- [Feature] **Manual UID shift override** - Added `disable_shift` config option for manual control in edge cases: `[incus]` `disable_shift = true` in `~/.config/coi/config.toml`. The auto-detection works in most cases, but this option allows manual override if needed.
- [Feature] Add `coi persist` command to convert ephemeral sessions to persistent - Allows converting running ephemeral containers to persistent mode, preventing automatic deletion when stopped. Supports `--all` flag to persist all containers and `--force` to skip confirmations. Use `coi list` to verify persistence mode.
- [Feature] **Display IPv4 addresses in `coi list`** - The `coi list` command now shows the IPv4 address (eth0) for running containers, making it easy to access exposed web servers and services. The IPv4 field appears in both text and JSON output formats. Stopped containers do not display an IP address since they have no network connectivity. (#66)

### Bug Fixes

- [Bug Fix] **Exotic terminal type support** - Fixed tmux failing with "missing or unsuitable terminal" error when using modern terminals like Ghostty, WezTerm, Alacritty, or Kitty. These terminals set TERM to values (e.g., `xterm-ghostty`) that don't exist in container terminfo databases. Added automatic mapping to `xterm-256color` while preserving standard terminal types unchanged. Applies to both environment TERM and `-e TERM=...` flag. (#53)

### Technical Details

OVN host routing:
- **Automatic Detection**: Checks if container's network is OVN type (vs standard bridge)
- **Route Configuration**: Extracts OVN subnet (from `ipv4.address`), OVN uplink IP (from `volatile.network.ipv4.address`), and uplink bridge name (from `network` config)
- **Key Insight**: Routes via OVN's uplink IP on the bridge (e.g., `10.47.62.100`) NOT the bridge's gateway IP (e.g., `10.47.62.1`). The OVN uplink IP acts as the gateway to the OVN network.
- **Graceful Degradation**: If route already exists, skips addition. If sudo fails, shows helpful command instead of failing container startup
- **Auto-healing**: Automatically checks and re-adds route on every container start, so routes are restored after reboot (requires sudo)
- **IP Stability**: OVN uplink IP is relatively stable (changes only if OVN network is deleted/recreated)
- **Persistence Options**: README documents three approaches for fully automatic routing after reboot: passwordless sudo (recommended for dev), systemd service, or netplan configuration
- **Clear Messaging**: Improved messages explain OVN routing is independent of network mode and works in open/restricted/allowlist modes
- **Use Case**: Essential for development workflows where users run web servers (Rails, Django, Node), databases (PostgreSQL, MySQL), or APIs in containers and need to access them from host browsers, database clients, or API testing tools
- **CI Integration**: GitHub Actions CI now properly configures OVN routing, enabling integration tests for network isolation features

Docker/nested container support:
- **Automatic Configuration**: All containers automatically receive Docker support flags on launch
- **Security Flags**: Three flags are set: `security.nesting=true` (enables nested containerization), `security.syscalls.intercept.mknod=true` (safe device node creation), `security.syscalls.intercept.setxattr=true` (safe filesystem attribute handling)
- **Security Model**: Incus's syscall interception provides defense-in-depth - even if a process escapes a Docker container, it remains within the isolated Incus container
- **Use Case**: Safe for development/testing environments (COI's primary use case) where you control the workloads running inside containers
- **Technical Root Cause**: Docker (runc) needs to access and reopen file descriptors for network-related sysctls when creating network namespaces. Without nesting flags, runc cannot perform these operations, resulting in "permission denied" errors when accessing `net.ipv4.ip_unprivileged_port_start`

Colima/Lima detection:
- **Auto-detection**: Checks `/proc/mounts` for virtiofs filesystem (characteristic of Lima VMs)
- **Fallback check**: Verifies if running as `lima` user (Lima VM default user)
- **Logging**: Clearly indicates when auto-detection activates vs manual configuration
- **Override**: Manual `disable_shift = true` config takes precedence over auto-detection

This prevents the error: `Error: Failed to start device "workspace": Required idmapping abilities not available` when running COI inside Colima/Lima VMs on macOS.

### Testing

- [Testing] Added Docker integration tests - Three test scenarios: (1) verify Docker nesting flags are automatically enabled on container launch, (2) verify Docker actually works inside containers without network namespace errors, (3) verify Docker fails with a clear error when required nesting flags are not enabled (regression test) (tests/docker/ directory).
- [Testing] Added integration tests for `coi persist` command - Five test scenarios covering basic operation, bulk operations, state verification, and error handling (tests/persist/ directory).
- [Testing] Added comprehensive terminal sanitization tests - Unit tests, integration tests with real tmux sessions, and CI end-to-end tests that verify exotic terminal types work correctly in containers.
- [Testing] Added integration tests for IPv4 display in `coi list` - Three test scenarios covering running containers showing IPv4, stopped containers not showing IPv4, and JSON format including the ipv4 field (tests/list/ directory).
- [Testing] Added comprehensive OVN routing integration tests - Tests validate automatic host route configuration, route persistence across container restarts, and proper cleanup on container deletion. Tests run in actual OVN network environment in CI to verify end-to-end functionality (tests/network/test_ovn_routing.py). (#81)

### CI/CD Improvements

- [CI/CD] **Parallel test execution for 2.6x faster CI** - Split integration test suite into 6 parallel test groups that run across 11 jobs (6 groups × 2 network types - 1 for network/bridge skip). Test groups: shell-ephemeral (15 tests), shell-persistent (9 tests), network (10 tests, OVN only), container-file (54 tests), core commands (83 tests), and misc commands (70 tests). **CI time reduced from 37 minutes to ~13 minutes**, providing 2.6x faster feedback loop for developers. All test groups run on both OVN and bridge networks to maintain full test coverage across both network types. (#81)
- [CI/CD] **Improved macOS Colima test reliability** - Added comprehensive retry logic for macOS Colima installation tests to handle two types of failures: (1) network failures during package downloads ("connection timed out"), and (2) build hangs during containerd/Docker setup. Implements 15-minute timeout per attempt with automatic retry on both timeout and network failures (up to 3 attempts). Prevents 45-minute CI hangs while maintaining resilience for transient Colima VM issues. (#81)


## 0.5.2 (2026-01-19)

### Bug Fixes

- [Bug Fix] Fix version mismatch in released binaries - Version 0.5.1 was incorrectly showing as 0.5.0 due to hardcoded version string in source code.

### Enhancements

- [Enhancement] Implement dynamic version injection via ldflags during build - Version is now automatically set from git tags at build time instead of being hardcoded in source code.
- [Enhancement] Add version verification step in GitHub Actions release workflow - Build process now validates that the binary version matches the git tag before creating releases, preventing future version mismatches.
- [Enhancement] Update Makefile to inject version from git tags using `git describe --tags --always --dirty`, with fallback to "dev" for local builds without tags.

### Technical Details

Version injection implementation:
- **Source code**: Changed `Version` from `const` to `var` with default value "dev" in `internal/cli/root.go`
- **Build system**: Added `VERSION` variable and `LDFLAGS` to Makefile for dynamic version injection
- **Release workflow**: Pass `VERSION` environment variable to build step and verify binary version matches expected tag
- **Verification**: Release workflow now extracts version from built binary and compares against git tag, failing build on mismatch

## 0.5.1 (2026-01-17)

### Features

- [Feature] Auto-detect and fix DNS misconfiguration during image build. On Ubuntu systems with systemd-resolved, containers may receive `127.0.0.53` as their DNS server, which doesn't work inside containers. COI now automatically detects this issue and injects working public DNS servers (8.8.8.8, 8.8.4.4, 1.1.1.1) to unblock the build process.
- [Feature] Built images now include conditional DNS fix that activates only when DNS is misconfigured, ensuring containers work regardless of host Incus network configuration.
- [Feature] Allowlist mode now supports raw IPv4 addresses in addition to domain names. Users can add entries like `8.8.8.8` directly to `allowed_domains` without needing to resolve them.

### Bug Fixes

- [Bug Fix] Suppress spurious "Error: The instance is already stopped" message during successful image builds. The error was appearing during cleanup when the container was already stopped by the imaging process. Now checks if container is running before attempting to stop it.
- [Bug Fix] Fix spurious "Error: The instance is already stopped" message during `coi run --persistent` cleanup. When a persistent container stopped itself after command completion, the cleanup tried to stop it again, causing spurious errors. Now checks if container is running before attempting to stop it.
- [Bug Fix] Fix potential race condition in `coi shutdown` where force-kill could attempt to stop an already-stopped container if graceful shutdown completed during the timeout window. Now checks if container is still running before attempting force-kill.

### Documentation

- [Docs] Added Troubleshooting section to README with DNS issues documentation and permanent fix instructions.

### Testing

- [Testing] Added integration test `tests/build/no_spurious_errors.py` to verify no spurious errors appear during successful builds
- [Testing] Added integration test `tests/run/run_persistent_no_spurious_errors.py` to verify no spurious errors during persistent run cleanup
- [Testing] Added integration test `tests/shutdown/shutdown_no_spurious_errors.py` to verify no spurious errors during shutdown with timeout
- [Testing] Added integration test `tests/build/build_dns_autofix.py` to verify DNS auto-fix works during builds with misconfigured DNS
- [Testing] Added unit test `internal/network/resolver_test.go` for raw IPv4 address support in allowlist mode

## 0.5.0 (2026-01-15)

**Major architectural refactoring to support multiple AI coding tools**

This release introduces a comprehensive tool abstraction layer that allows code-on-incus to support multiple AI coding assistants beyond Claude Code. The refactoring was completed in three phases (Phase 1-3) with minimal user-facing changes.

### Breaking Changes

**Session Directory Structure:**
- Old: `~/.coi/sessions/<session-id>/`
- New: `~/.coi/sessions-claude/<session-id>/` (for Claude)
      `~/.coi/sessions-aider/<session-id>/` (for Aider, future)
      etc.

**Migration:** Old sessions in `~/.coi/sessions/` will not be automatically migrated. You can manually move session directories if needed, or start fresh sessions.

### Features

**Phase 1: Tool Abstraction Layer (#18)**
- [Feature] New `tool.Tool` interface for AI coding tool abstraction
- [Feature] `ClaudeTool` implementation with session discovery and command building
- [Feature] Tool registry system for registering and retrieving tools
- [Feature] Config-based tool selection via `tool.name` configuration option

**Phase 2: Runtime Integration (#19)**
- [Feature] Tool abstraction wired throughout runtime (shell, setup, cleanup)
- [Feature] Tool-specific configuration directory handling (e.g., `.claude`, `.aider`)
- [Feature] Tool-specific sandbox settings injection
- [Feature] Support for both config-based and ENV-based tool authentication

**Phase 3: Tool-Specific Session Directories (#20)**
- [Feature] Separate session directories per tool (`sessions-claude`, `sessions-aider`)
- [Feature] Session isolation between different AI tools
- [Feature] Extensible architecture for adding new tools without affecting existing sessions

### Configuration

New `tool` configuration section:
```toml
[tool]
name = "claude"          # AI coding tool to use (currently supports: claude)
# binary = "claude"      # Optional: override binary name
```

### Code Quality & Testing

- [Enhancement] Added golangci-lint to CI with essential linters
- [Enhancement] Added race detector to Go unit tests (`-race` flag)
- [Enhancement] Added test coverage reporting (local, no third-party uploads)
- [Enhancement] Auto-formatted entire codebase with gofmt/gofumpt
- [Enhancement] Removed unused code and functions

### Documentation

- [Documentation] Updated README from "claude-on-incus" to "code-on-incus"
- [Documentation] Rebranded to emphasize multi-tool support
- [Documentation] Added "Supported AI Coding Tools" section
- [Documentation] Updated all CLI help text to be tool-agnostic
- [Documentation] Noted Claude Code as default tool with extensibility for others

### Technical Details

**Tool Interface:**
```go
type Tool interface {
    Name() string                  // "claude", "aider", "cursor"
    Binary() string                // binary name to execute
    ConfigDirName() string         // config directory (e.g., ".claude")
    SessionsDirName() string       // sessions directory name
    BuildCommand(...) []string     // build CLI command
    DiscoverSessionID(...) string  // find session ID from state
    GetSandboxSettings() map[string]interface{}  // sandbox settings
}
```

### New Files
- `internal/tool/tool.go` - Tool abstraction interface and Claude implementation
- `internal/tool/registry.go` - Tool registry for factory pattern
- `internal/tool/tool_test.go` - Comprehensive tool abstraction tests
- `internal/session/paths.go` - Tool-specific session directory helpers

### Modified Files
- `internal/cli/shell.go` - Tool-aware session management
- `internal/cli/list.go` - Tool-specific session listing
- `internal/cli/info.go` - Tool-specific session info
- `internal/cli/clean.go` - Tool-specific session cleanup
- `internal/cli/root.go` - Updated CLI descriptions to be tool-agnostic
- `internal/cli/attach.go` - Generic "AI coding session" terminology
- `internal/cli/build.go` - Multi-tool support noted
- `internal/cli/tmux.go` - Generic session references
- `internal/session/setup.go` - Tool-aware setup logic
- `internal/session/cleanup.go` - Tool-aware cleanup logic
- `internal/config/config.go` - Added ToolConfig section
- `.golangci.yml` - Comprehensive linter configuration
- `.github/workflows/ci.yml` - Added golangci-lint, race detector, coverage
- `README.md` - Rebranded to emphasize multi-tool support

### Future Tool Support

The architecture now supports adding new AI coding tools with minimal changes:
1. Implement the `Tool` interface
2. Register in `tool/registry.go`
3. Tool-specific sessions automatically isolated

Example tools that can be added:
- Aider - AI pair programming assistant
- Cursor - AI-first code editor
- Any CLI-based AI coding assistant

## 0.4.0 (2026-01-14)

Add comprehensive network isolation with domain allowlisting and IP-based filtering, enabling high-security environments where containers can only communicate with approved domains.

### Features
- [Feature] Domain allowlisting mode - Restrict container network access to only approved domains
- [Feature] DNS resolution with automatic IP refresh (every 30 minutes by default)
- [Feature] IP caching for DNS failure resilience and container restarts
- [Feature] Background goroutine for periodic IP refresh without container restart
- [Feature] Per-profile domain allowlists for different security contexts

### Enhancements
- [Enhancement] New `allowlist` network mode alongside existing `restricted` and `open` modes
- [Enhancement] Always block RFC1918 private networks in allowlist mode
- [Enhancement] Persistent IP cache at `~/.coi/network-cache/<container>.json`
- [Enhancement] Graceful DNS failure handling with last-known-good IPs
- [Enhancement] Comprehensive logging for DNS resolution and IP refresh operations
- [Enhancement] Dynamic ACL recreation for IP updates without container restart

### Configuration
- `network.mode = "allowlist"` - Enable domain allowlisting
- `network.allowed_domains = ["github.com", "api.anthropic.com"]` - List of allowed domains
- `network.refresh_interval_minutes = 30` - IP refresh interval (default: 30, 0 to disable)

### Documentation
- [Documentation] Updated README.md with network isolation modes and configuration
- [Documentation] Added DNS failure handling and IP refresh behavior explanations
- [Documentation] Documented security limitations and best practices
- [Documentation] Simplified networking documentation for better accessibility

### Technical Details
Allowlist implementation:
- **DNS Resolution**: Resolves domains to IPv4 addresses on container start
- **ACL Structure**: Default-deny with explicit allow rules for resolved IPs
- **IP Refresh**: Background goroutine checks for IP changes every 30 minutes
- **Cache Format**: JSON file with domain-to-IPs mapping and last update timestamp
- **Graceful Degradation**: Uses cached IPs on DNS failures, only fails if no IPs ever resolved
- **ACL Update**: Full ACL recreation (delete + create + reapply) for IP changes (~100ms network interruption)

### New Files
- `internal/network/cache.go` - IP cache persistence manager
- `internal/network/resolver.go` - DNS resolver with caching and fallback
- `tests/network/test_allowlist.py` - Integration test framework for allowlist mode

### Modified Files
- `internal/config/config.go` - Added `AllowedDomains`, `RefreshIntervalMinutes`, `NetworkModeAllowlist`
- `internal/network/acl.go` - Added `CreateAllowlist()`, `buildAllowlistRules()`, `RecreateWithNewIPs()`
- `internal/network/manager.go` - Added `setupAllowlist()`, `startRefresher()`, `stopRefresher()`, `refreshAllowedIPs()`
- `README.md` - Added network isolation section with all three modes
- `.github/workflows/ci.yml` - Increased storage pool from 5GiB to 15GiB
- `tests/meta/installation_smoke_test.py` - Added retry logic for transient network issues

## 0.3.2 (2026-01-14)

Add network isolation to prevent containers from accessing local/internal networks while allowing full internet access for development workflows.

### Features
- [Feature] Network isolation - Block container access to private networks (RFC1918) and cloud metadata endpoints by default
- [Feature] `--network` flag to control network mode: `restricted` (default) or `open`
- [Feature] Dynamic gateway discovery in tests to work on any network configuration
- [Feature] Comprehensive network isolation test suite (6 tests covering restricted/open modes)

### Bug Fixes
- [Fix] Dummy image build - Fix `buildCustom()` to push dummy file to container, enabling test image builds
- [Fix] Incus ACL configuration - Add explicit `egress action=allow` rule to prevent default deny behavior

### Enhancements
- [Enhancement] Network documentation - Add comprehensive `NETWORK.md` with security model, configuration, and testing guide
- [Enhancement] Two-step ACL application - Use `device override` followed by `device set` for proper ACL attachment
- [Enhancement] Integration tests use backgrounded containers for consistency and reliability
- [Enhancement] README updated with network isolation section and security information

### Technical Details
Network isolation implementation:
- **Restricted mode (default)**: Blocks RFC1918 ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) and cloud metadata (169.254.0.0/16), allows all public internet
- **Open mode**: No restrictions (previous behavior)
- **Implementation**: Incus network ACLs applied at container network interface level
- **Tests**: 6 integration tests validate blocking private networks, metadata endpoints, and local gateway while allowing internet access

## 0.3.1 (2026-01-13)

Re-release of 0.3.0 with proper GitHub release automation.

## 0.3.0 (2026-01-13)

Add machine-readable output formats to enable programmatic integration with claude_yard Ruby project.

### Features
- [Feature] Add `--format=json` flag to `coi list` command for machine-readable output
- [Feature] Add `--format=raw` flag to `coi container exec --capture` for raw stdout output (exit code via $?)

### Bug Fixes
- [Fix] Power management permissions - Add wrapper scripts for shutdown/poweroff/reboot commands to work without sudo prefix (uses passwordless sudo internally)

### Enhancements
- [Enhancement] Enable programmatic integration between coi and claude_yard projects
- [Enhancement] Add 5 integration tests for new output formats (3 for list, 2 for exec)
- [Enhancement] Add integration test for power management commands without sudo
- [Enhancement] Update README with --format flag documentation and examples
- [Enhancement] Normalize all "fake-claude" references to "dummy" throughout codebase (tests, docs, scripts)
- [Enhancement] Remove FAQ.md - content no longer relevant after refactoring

## 0.2.0 (2026-01-03)

Major internal refactoring to make coi CLI-agnostic (zero breaking changes). Enables future support for tools beyond Claude Code (e.g., Aider, Cursor). Includes bug fixes for persistent containers, slot allocation, and CI improvements.

### Features
- [Feature] Add `shutdown` command for graceful container shutdown (separate from `kill`)
- [Feature] Add `attach` command to attach to running sessions
- [Feature] Add `images` command to list available Incus images
- [Feature] Add `version` command for displaying version information
- [Feature] Add GitHub Actions workflow for automated releases with pre-built binaries
- [Feature] Add automatic `~/.claude` config mounting (enabled by default)
- [Feature] Add CHANGELOG.md for version history tracking
- [Feature] Add one-shot installer script (`install.sh`)

### Refactoring (Internal API - Non-Breaking)
- [Refactor] Rename functions: `runClaude()` → `runCLI()`, `runClaudeInTmux()` → `runCLIInTmux()`, `GetClaudeSessionID()` → `GetCLISessionID()`, `setupClaudeConfig()` → `setupCLIConfig()`
- [Refactor] Rename variables: `claudeBinary` → `cliBinary`, `claudeCmd` → `cliCmd`, `claudeDir` → `stateDir`, `claudePath` → `statePath`, `claudeJsonPath` → `stateConfigPath`
- [Refactor] Rename struct fields: `ClaudeConfigPath` → `CLIConfigPath`
- [Refactor] Rename test infrastructure: "fake-claude" → "dummy", `COI_USE_TEST_CLAUDE` → `COI_USE_DUMMY`
- [Refactor] Update all internal documentation to use generic "CLI tool" terminology

### Bug Fixes
- [Fix] Persistent container filesystem persistence - Files now survive container stop/start
- [Fix] Resume flag inheritance - `--resume` properly inherits persistent/privileged flags from session metadata
- [Fix] Slot allocator race condition - Improved slot allocation logic to prevent conflicts
- [Fix] Environment variable passing in `run` command - Variables now properly passed to containers
- [Fix] Attach command container detection - Improved reliability of attach operations
- [Fix] CI networking issues - Better timeout handling (180s) and diagnostics for slower environments
- [Fix] Test suite stability - Various fixes to make tests more reliable and deterministic
- [Fix] Persistent container indicator in `coi list` - Shows "(persistent)" label correctly
- [Fix] CI cache key updated to use `testdata/dummy/**` pattern
- [Fix] Documentation inconsistencies between README and actual implementation
- [Fix] **Tmux server persistence in CI** - Explicitly start tmux server before session operations; ensures sessions work in CI and new containers
- [Fix] **Test isolation for parallel execution** - Fixed auto_attach_single_session test to use --slot flag, preventing conflicts when other sessions are running

### Enhancements
- [Enhancement] Update image builder to use `dummy` instead of `test-claude`
- [Enhancement] Improve CI networking with HTTP/HTTPS fallback tests
- [Enhancement] Add backwards-compatible test fixtures (`fake_claude_path` → `dummy_path`)
- [Enhancement] Update dummy script with generic terminology and documentation
- [Enhancement] Improve README with complete command documentation (attach, images, version, shutdown)
- [Enhancement] Update configuration examples with `mount_claude_config` option
- [Enhancement] Document `--storage` flag in README
- [Enhancement] Add refactoring documentation (CLAUDE_REFERENCES_ANALYSIS.md, REFACTORING_SUMMARY.md, REFACTORING_PHASE2.md)
- [Enhancement] Add "See Also" section in README with links to documentation
- [Enhancement] **Tmux architecture** - Sessions created detached then attached separately; tmux server explicitly started before operations for reliability
- [Enhancement] **Python linting with ruff** - Added ruff linter (Python equivalent of rubocop) to CI, auto-fixed 68 issues, formatted 166 test files for consistency
- [Enhancement] **CI tests now run all attach tests** - Removed skipif decorators after fixing tmux persistence, all tests pass in CI

### Changes
- [Change] Rename images from `claudeyard-*` to `coi-*` for consistency
- [Change] **Session creation pattern** - Changed from `tmux new-session` (single command) to `tmux new-session -d` + `tmux attach` (two-step pattern) for better detach/reattach support

## 0.1.0 (2025-12-11)

Initial release of claude-on-incus (coi) - Run Claude Code in isolated Incus containers.

### Core Features

- [Feature] Multi-slot support for running parallel Claude sessions on same workspace
- [Feature] Session persistence with `.claude` directory restoration
- [Feature] Persistent container mode to keep containers alive between sessions
- [Feature] Workspace isolation with automatic mounting
- [Feature] TOML-based configuration system with profile support
- [Feature] Automatic UID mapping for correct file permissions (no permission hell)
- [Feature] Environment variable passing to containers
- [Feature] Persistent storage mounting across sessions

### CLI Commands

- [Feature] `shell` command - Interactive Claude sessions with full resume support
- [Feature] `run` command - Execute commands in ephemeral containers
- [Feature] `build` command - Build sandbox and privileged Incus images
- [Feature] `list` command - List active containers and saved sessions
- [Feature] `info` command - Show detailed session information
- [Feature] `clean` command - Clean up stopped containers and old sessions
- [Feature] `tmux` command - Tmux integration for background processes

### Container Images

- [Feature] Sandbox image (`coi-sandbox`) - Ubuntu 22.04 + Docker + Node.js + Claude CLI + tmux
- [Feature] Privileged image (`coi-privileged`) - Sandbox + GitHub CLI + SSH + Git config
- [Feature] Automatic container lifecycle management (ephemeral vs persistent)

### Configuration

- [Feature] Configuration hierarchy: built-in defaults → system → user → project → env vars → CLI flags
- [Feature] Named profiles with environment override support
- [Feature] Project-specific configuration (`.claude-on-incus.toml`)
- [Feature] User configuration (`~/.config/claude-on-incus/config.toml`)

### Session Management

- [Feature] Automatic session saving on exit
- [Feature] Resume from previous sessions with `--resume` flag
- [Feature] Session auto-detection (resume latest session for workspace)
- [Feature] Graceful Ctrl+C handling with cleanup
- [Feature] Session metadata tracking (workspace, slot, timestamp, flags)

### Testing

- [Feature] Comprehensive integration test suite (3,900+ lines)
- [Feature] CLI command tests for all commands
- [Feature] Feature scenario tests for workflows
- [Feature] Error handling tests for edge cases

### Documentation

- [Feature] Comprehensive README with Quick Start guide
- [Feature] Why Incus vs Docker comparison section
- [Feature] Architecture diagrams and explanations
- [Feature] Configuration examples and hierarchy documentation
- [Feature] Persistent mode guide (`PERSISTENT_MODE.md`)
- [Feature] Integration testing documentation (`INTE.md`)
