# CHANGELOG

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
