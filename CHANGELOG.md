# CHANGELOG

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
