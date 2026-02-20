# CHANGELOG

## Unreleased

### Bug Fixes

- [Bug Fix] **Security monitoring no longer spams alerts or corrupts TUI** - Fixed multiple issues with security monitoring: (1) The responder now tracks pause/kill state to prevent infinite retry loops when container is already frozen. (2) Added threat deduplication with 30-second window to prevent alert spam. (3) Removed all debug log statements that corrupted the coding agent TUI. (4) All monitoring output now goes to audit log file only. (5) Added `OnAction` callback that only prints to stderr when container is actually paused or killed - the only output users will see. (6) Added double-check filter in nftables log reader to ensure source IP matches container IP. (7) Added `IncusOutputWithStderr` function to properly capture Incus error messages like "already frozen" which are written to stderr. (8) Added comprehensive integration tests for responder and resume command.

- [Bug Fix] **Firewall rule accumulation causing system hang** - Fixed multiple issues causing firewall rules to accumulate to 100k+ entries, eventually hanging the system: (1) Signal handler used `os.Exit()` which skips deferred cleanup - now explicitly calls cleanup before exit. (2) `coi shutdown` was missing firewall cleanup (only `coi kill` had it). (3) `coi clean --orphans` was reloading firewalld after removing zone bindings, which wiped Docker's dynamically-added nft rules - removed the unnecessary reload. (4) RFC1918 addresses were incorrectly flagged as suspicious in "open" network mode (no restrictions), causing false-positive container freezes from host traffic like Synology Drive. Includes comprehensive integration tests for firewall cleanup verification across all network modes and cleanup scenarios.

- [Bug Fix] **Docker commands now work without sudo for the code user** - The `code` user was correctly added to the `docker` supplementary group, but `incus exec --group` may not call `initgroups()` to load supplementary groups depending on the Incus version/configuration, leaving the docker socket inaccessible without `sudo`. Fixed with two layers: (1) `/etc/docker/daemon.json` sets `"group": "code"` so Docker chowns the socket to `root:code` on startup; (2) a `docker.socket` systemd drop-in sets `SocketGroup=code` so Ubuntu's socket-activation path also creates the socket with the correct group. The `code` user's primary group (GID 1000) is always active regardless of how the incus session was started, so `docker` works without `sudo` in all session types. Adds a test that explicitly runs `docker run` as UID 1000 / GID 1000 (replicating the real-world `incus exec --user 1000 --group 1000` call) to catch regression. Fixes #134.

- [Bug Fix] **Fix /tmp exhaustion causing agents to hang silently** - Coding agents that run heavy builds (npm, cargo, Docker) could fill `/tmp` and freeze indefinitely with no error. `/tmp` is now backed by the container's virtual root disk by default (no RAM cap, shares the storage pool allocation) instead of an artificially small RAM-backed tmpfs. The previous `SetTmpfsSize()` implementation was also corrected — it was setting `limits.memory.tmpfs`, an invalid Incus config key that was silently dropped; it now uses the correct `size=` device property. The RAM-backed tmpfs mode remains available as opt-in via `tmpfs_size = "4GiB"` under `[limits.disk]`. Config merging now correctly propagates `tmpfs_size` overrides. The base image ships `/etc/tmpfiles.d/coi-tmp-cleanup.conf` (1-hour age threshold) and a `systemd-tmpfiles-clean.timer` override (15-minute interval) so abandoned artefacts are reclaimed automatically. Includes unit and integration tests. Wiki Troubleshooting and FAQ pages updated. Fixes #135.

### Features

- [Feature] **`coi resume` command** - Added command to resume containers paused by the security monitoring system. Use `coi resume <container-name>` to resume a specific frozen container, or `coi resume` to resume all frozen COI containers. Only works with containers that are in the Frozen state.

- [Feature] **`--tool` flag for `coi shell`** - Added `--tool` flag to override the configured AI tool for a single session without creating or modifying `.coi.toml`. Example: `coi shell --tool opencode`. Takes precedence over `tool.name` in config files.

- [Feature] **Incus storage pool health check** - Added `incus_storage_pool` check to `coi health` that reports used/free/total space in the Incus storage pool (shared by all images, containers, and snapshots). Detects the pool name from the default Incus profile automatically. Warns when free space drops below 5 GiB or usage exceeds 80%, fails below 2 GiB or above 90%. Includes 2 integration tests.

- [Feature] **opencode support** - Added opencode (https://opencode.ai) as a supported AI coding tool. opencode is installed in the default base image alongside Claude CLI. Configure it with `[tool] name = "opencode"` in `.coi.toml` or use `coi shell --tool opencode`. COI automatically copies `~/.opencode.json` from the host (if present) into the container and merges the permission bypass config (`"permission": {"*": "allow"}`) so opencode runs without interactive permission prompts. **Note:** Unlike Claude (which shows a login screen), opencode requires API keys to start — set `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` environment variable, or configure providers in `~/.opencode.json`. Resume support uses `--continue` to resume the last SQLite session. Implements the new `ToolWithHomeConfigFile` optional interface for tools that use a single home-dir JSON file rather than a config subdirectory. Includes 9 unit tests and 2 integration tests. Fixes #117.

- [Feature] **Base image includes ripgrep and fzf** - Added `ripgrep` (rg) and `fzf` to the default base image dependencies. These are used by opencode and other tools for fast file searching and fuzzy finding.

- [Feature] **nftables-based network monitoring** - Added event-driven network monitoring using nftables and systemd-journal for complete visibility into container network activity. Replaces polling-based /proc/net/tcp monitoring with kernel-level packet filtering that captures all network events including short-lived connections (<2s), blocked connection attempts, and DNS queries. Features real-time threat detection for RFC1918 addresses, metadata endpoint access (169.254.169.254), suspicious ports (4444, 5555, 31337), allowlist violations, and DNS query anomalies. Monitoring daemon runs automatically during sessions when `[monitoring.nft]` is enabled (default: true). Logs are streamed from journald with configurable rate limiting (default: 100 packets/second for normal traffic, unlimited for suspicious traffic). Includes health checks for nftables, systemd-journal access, and libsystemd-dev installation. Setup script provided at `scripts/install-nft-deps.sh` to configure system dependencies, group membership, and passwordless sudo for nft commands. Works on Lima/macOS via limactl wrapper. Audit logs stored at `~/.coi/audit/<container-name>-nft.jsonl`. This provides kernel-level security monitoring that can't be tampered with from inside the container, catching threats that process-level monitoring might miss.

### Documentation

- [Documentation] **Image management operations** - Added inline examples to README for existing `coi image` commands (list, publish, exists, delete, cleanup). These commands have been available since early releases for creating and managing custom container images, with 20 integration tests and complete wiki documentation. Quick reference examples now included in README for common operations like publishing containers as images, checking image existence, and cleaning up old image versions. See [Image Management wiki page](https://github.com/mensfeld/code-on-incus/wiki/Image-Management) for detailed usage examples and workflows.
- [Documentation] **File transfer operations** - Documented existing `coi file push` and `coi file pull` commands in README. These commands have been available since early releases for transferring files and directories between host and containers, supporting recursive operations with the `-r` flag. Includes 16 integration tests covering various scenarios. See [File Transfer wiki page](https://github.com/mensfeld/code-on-incus/wiki/File-Transfer) for detailed usage examples.
- [Documentation] **nftables monitoring setup** - Added comprehensive documentation for nftables-based network monitoring including system requirements, setup instructions, configuration options, and verification steps. Includes inline examples in README for installing dependencies (libsystemd-dev, nftables), configuring group membership and passwordless sudo, and verifying the setup with `coi health`. Documents the architecture, threat detection capabilities, and audit logging format.

### Bug Fixes

- [Bug Fix] **Automatic cleanup of orphaned firewalld zone bindings** - Added detection and cleanup of orphaned veth interface zone bindings in firewalld that accumulate when containers are deleted without proper cleanup. COI now automatically removes veth zone bindings during container deletion (via `coi kill` and `coi clean`) and provides `coi clean --orphans` to detect and remove orphaned resources including: veth interfaces (no master bridge), firewall rules (non-existent container IPs), and firewalld zone bindings (veths in zones but not on system). This prevents firewalld from accumulating stale interface bindings over time, which could cause configuration bloat and potential conflicts. Includes integration tests for firewall cleanup functionality. (#130)
- [Bug Fix] **Fix firewall rule cleanup on container delete** - The `coi container delete` command was missing firewall rule cleanup, causing rule accumulation when containers were deleted (especially during test cleanup). Now properly gets container IP before deletion and removes all firewall rules for that IP. This completes the firewall cleanup fixes ensuring all container deletion paths (`coi kill`, `coi container delete`, `session.Cleanup()`) properly clean up firewall rules. Fixes #119. (#120)
- [Bug Fix] **Fix flaky installation smoke test** - Fixed `test_full_installation_process` failing intermittently when trying to clone PR branches that don't exist in the main repository. This occurred for fork PRs (branch only exists in fork) or after branches were deleted post-merge. The test now tries the specific branch first and falls back to the default branch if unavailable. (#121)
- [Bug Fix] **Fix flaky allowlist network blocking tests** - Fixed `test_allowlist_blocks_non_allowed_dns` failing intermittently because it ran immediately after container startup without waiting for firewalld rules to fully propagate. In CI environments, firewalld rule propagation can be slow, causing traffic to 9.9.9.9 (Quad9 DNS) to succeed when it should be blocked. Added `time.sleep(5)` before all blocking tests and increased existing sleeps from 2s to 5s for consistency. (#122)
- [Bug Fix] **Handle cross-device link and symlinks when saving session data** - Fixed `EXDEV` (cross-device link) error when `/tmp` and session directory are on different filesystems. `PullDirectory` now falls back to recursive copy when `os.Rename()` fails with cross-device error. Added proper symlink handling - symlinks (e.g., `.claude/debug/latest`) are now correctly detected and recreated instead of being treated as regular files. This prevents session save failures on systems where temp storage and session storage are on different mount points. Thanks @psaab! (#106)
- [Bug Fix] **Increased test timeout values for CI reliability** - Comprehensively increased timeouts across all ephemeral shell tests to improve CI reliability. Container deletion timeout increased from 30s to 90s, container operations from 30s to 90s, network teardown from 60s to 120s, and other operations from 30s to 90s. CI environments need significantly more time for container cleanup after poweroff, container deletion operations, and network teardown operations. This fixes all timing-related test failures in shell-ephemeral tests.

### Features

- [Feature] **Configurable security protection for sensitive paths** - Extended the security protection feature to support a configurable list of paths mounted read-only by default. This prevents containers from modifying files that could execute automatically on the host, protecting against supply chain attacks where a compromised AI tool could inject malicious code. Default protected paths:
  - `.git/hooks` - Git hooks execute on git operations (commits, pushes, etc.)
  - `.git/config` - Can set `core.hooksPath` to bypass hooks protection
  - `.husky` - Husky git hooks manager directory
  - `.vscode` - VS Code `tasks.json` can auto-execute, `settings.json` can inject shell args

  Configuration options in `[security]` section:
  - `protected_paths` - Replace default protected paths list entirely
  - `additional_protected_paths` - Add paths without replacing defaults (e.g., `[".idea", "Makefile"]`)
  - `disable_protection` - Disable all path protection (not recommended)

  The `--writable-git-hooks` flag still works for backwards compatibility, disabling all protection when set. Symlinks are rejected for security (prevents mounting arbitrary host paths). Git worktrees and submodules (where `.git` is a file or symlink) are safely handled. Includes comprehensive unit tests for all protection scenarios.
- [Feature] **Security Monitoring System** - Added always-on security monitoring to detect and respond to malicious behavior in real-time. The monitoring daemon runs automatically during sessions and protects against reverse shells, data exfiltration attempts, environment scanning for secrets, and unexpected network connections. Features automated response based on threat severity (log → alert → pause → kill), persistent audit logging in JSON Lines format at `~/.coi/audit/<container-name>.jsonl`, and real-time threat detection using pattern matching and heuristics. New `coi monitor` command provides real-time monitoring dashboard with threat detection, and `coi monitor audit` command allows reviewing historical security events. Configuration available via `~/.config/coi/config.toml` under `[monitoring]` section with thresholds for file read rates, automated pause/kill settings, and audit log retention. Implements security-first design to protect against prompt injection attacks that attempt to exfiltrate company data, establish reverse shells, or scan for secrets despite container isolation. Addresses #112.
- [Feature] **Container list command** - Added `coi container list` command to provide raw container listing with JSON or text format support. This is a low-level command similar to `incus list`, designed for programmatic use and automation. Supports `--format=json` for structured output and `--format=text` (default) for human-readable table format. Essential for coi-pond admin status checks and other tools that need to query container state. Includes integration tests for both formats and error handling. (#124)
- [Feature] **PTY allocation support for container exec** - Added `-t/--tty` flag to `coi container exec` command to enable pseudo-terminal (PTY) allocation for interactive sessions. Essential for running interactive shells, tmux sessions, and other terminal applications that require PTY support. The flag wires to the existing `Interactive` field in `ExecCommandOptions` and uses the `--force-interactive` backend support. Validates that `--tty` and `--capture` are mutually exclusive since PTY allocation requires stdin/stdout/stderr connectivity. Includes integration tests for PTY allocation verification and flag conflict validation. This enables coi-pond and other tools to use coi as a complete abstraction layer instead of calling incus directly for interactive sessions. (#123)
- [Feature] **Container connectivity health check** - Added `container_connectivity` check to `coi health` command that tests actual internet connectivity from inside a container. Launches an ephemeral test container, runs DNS resolution (`getent hosts api.anthropic.com`) and HTTP connectivity (`curl https://api.anthropic.com`) tests, then cleans up. This catches real networking issues like DHCP failures, DNS misconfiguration, or firewall problems that the existing host-level checks miss. The check runs by default (not just with `--verbose`) since container networking issues are critical for COI to function. Returns OK if both tests pass, Warning if one fails, or Failed if both fail. Includes integration tests for image-not-found scenarios and cleanup verification. (#102)
- [Feature] **Network restriction health check** - Added `network_restriction` check to `coi health` that verifies restricted network mode is actually blocking private networks. Launches a test container, applies firewall rules, then tests that: (1) external internet (api.anthropic.com) IS accessible, and (2) RFC1918 private IPs (10.x.x.x, 192.168.x.x) ARE blocked. This catches firewall misconfigurations where "restricted" mode isn't actually restricting anything. Runs by default (skipped with warning if firewalld not available). Includes integration tests for cleanup verification. (#102)

## 0.6.0 (2026-02-02)

### Bug Fixes

- [Bug Fix] **Settings.json merge instead of overwrite** - Fixed critical bug where `~/.claude/settings.json` was being completely overwritten with sandbox settings, losing all user configurations like AWS Bedrock credentials, environment variables, and custom settings. The tool now properly merges sandbox settings into existing user settings (using the same pattern as `.claude.json`), preserving user configurations while adding necessary sandbox permissions. This enables AWS Bedrock support and any other user-configured settings to work correctly inside containers. Added comprehensive test coverage to prevent regression. (#76)
- [Bug Fix] **`coi list --all` always shows Saved Sessions section** - Fixed bug where "Saved Sessions:" section would not appear when using `--all` flag if no sessions with saved state existed. The function `listSavedSessions()` was returning `nil` instead of an empty slice, causing the section to be skipped entirely. Now properly initializes as empty slice so the section always appears with `--all`, showing "(none)" when empty. This makes the output predictable and consistent. (#81)
- [Bug Fix] **Tool-agnostic session listing** - Fixed `coi list --all` hardcoding `.claude` directory check, which broke support for other AI coding tools (Aider, Cursor, etc.). Now uses `tool.ConfigDirName()` to dynamically check for the configured tool's config directory (e.g., `.aider/`, `.cursor/`). Also handles ENV-based tools (no config directory) by only checking for `metadata.json`. This ensures saved sessions are properly detected regardless of which AI tool is configured. (#81)
- [Bug Fix] **Test isolation improvements** - Fixed `test_list_format_json_empty` and `test_attach_shows_sessions` failing intermittently due to containers left by previous tests in random test order. Added `cleanup_containers` fixture and explicit `kill --all --force` before test execution to ensure clean state. This prevents test interference from pytest-randomly's randomized execution order. (#81)
- [Bug Fix] **macOS Colima build timeout handling** - Fixed macOS Colima installation tests hanging for 45 minutes when `coi build` gets stuck during containerd/Docker setup. Added 15-minute timeout per build attempt with automatic retry on timeout (exit code 124) in addition to existing network failure retry logic. Prevents CI jobs from hanging until job timeout - now retries after 15 minutes for transient Colima VM issues. (#81)
- [Bug Fix] **Improved DNS auto-fix during image build** - Extended DNS misconfiguration detection to handle more cases: localhost DNS (`127.0.0.1`), any `127.x.x.x` addresses, and empty/missing nameserver configurations. Previously only detected `127.0.0.53` (systemd-resolved stub). Now triggers DNS fix after 5 seconds instead of 10 for faster builds. Logs specific reason for the fix (localhost DNS, stub resolver, or missing config). Fixes issue where `coi build` would hang on "Waiting for network..." when host DNS points to localhost. (#83)

### Features

- [Feature] **AWS Bedrock validation for Colima/Lima** - Added automatic validation of AWS Bedrock configuration when running in Colima/Lima environments. COI now detects Bedrock setup in `settings.json` and validates: AWS CLI availability, dual `.aws` path detection (Lima VM home vs macOS home mounted via virtiofs), SSO cache file permissions (warns if too restrictive), AWS credential validity, and proper `.aws` mount configuration. Fails fast with clear, actionable error messages if setup is incomplete, preventing confusing authentication failures. Includes warnings about dual-path sync issues with guidance to use macOS path consistently. Validation runs automatically before container startup, ensuring users can't proceed with broken configurations. Closes #76.
- [Feature] **Resource and time limits for containers** - Added comprehensive resource control with CPU, memory, disk I/O, and runtime limits. Configure via TOML config file (`[limits]` section), profiles (`[profiles.name.limits]`), or CLI flags (`--limit-cpu`, `--limit-memory`, `--limit-duration`, etc.). Supports CPU count/allowance/priority, memory limit/enforce/swap, disk read/write/max rates, max processes, and automatic container shutdown after max runtime. Limits are validated client-side with clear error messages. Time limits use a background monitor that gracefully or forcefully stops containers when max_duration is reached. Configuration precedence: CLI flags > profile limits > config file > unlimited. Empty values default to unlimited for maximum flexibility. Closes #71.
- [Feature] **`coi health` command** - New system health check command that verifies all dependencies and reports their status. Checks OS info, Incus availability, permissions, default image, image age, network bridge, IP forwarding, firewalld, storage directories, disk space, configuration, active containers, and saved sessions count. Supports `--format json` for scripting/automation and `--verbose` for additional checks (DNS resolution, passwordless sudo). Returns exit codes: 0 (healthy), 1 (degraded with warnings), 2 (unhealthy with failures). Warns if image is older than 30 days or disk space is below 5GB. Helps diagnose setup issues and verify environment configuration.
- [Feature] **Firewalld-based network isolation** - Replaced OVN-based network ACLs with firewalld direct rules for network isolation. This simplifies the setup significantly - no more OVN/OVS dependencies. Network isolation (restricted/allowlist modes) now works with any standard Incus bridge network using firewalld's FORWARD chain filtering. Rules are scoped by container IP address for precise filtering and automatically cleaned up when containers stop. Requires firewalld to be installed and running (`sudo apt install firewalld && sudo systemctl enable --now firewalld`).
- [Feature] **Automatic Docker/nested container support** - COI now automatically enables Docker and container nesting support on all containers by setting `security.nesting=true`, `security.syscalls.intercept.mknod=true`, and `security.syscalls.intercept.setxattr=true`. This eliminates the "unable to start container process: error during container init: open sysctl net.ipv4.ip_unprivileged_port_start file: reopen fd 8: permission denied" error when running Docker inside Incus containers. No configuration required - Docker just works out of the box.
- [Feature] **Automatic Colima/Lima environment detection** - COI now automatically detects when running inside a Colima or Lima VM and disables UID shifting. These VMs already handle UID mapping at the VM level via virtiofs, making Incus's `shift=true` unnecessary and problematic. Detection checks for virtiofs mounts in `/proc/mounts` and the `lima` user. Users no longer need to manually configure `disable_shift` option.
- [Feature] **Manual UID shift override** - Added `disable_shift` config option for manual control in edge cases: `[incus]` `disable_shift = true` in `~/.config/coi/config.toml`. The auto-detection works in most cases, but this option allows manual override if needed.
- [Feature] Add `coi persist` command to convert ephemeral sessions to persistent - Allows converting running ephemeral containers to persistent mode, preventing automatic deletion when stopped. Supports `--all` flag to persist all containers and `--force` to skip confirmations. Use `coi list` to verify persistence mode.
- [Feature] **Display IPv4 addresses in `coi list`** - The `coi list` command now shows the IPv4 address (eth0) for running containers, making it easy to access exposed web servers and services. The IPv4 field appears in both text and JSON output formats. Stopped containers do not display an IP address since they have no network connectivity. (#66)
- [Feature] **`coi snapshot` command for container checkpoint management** - Comprehensive snapshot management for Incus containers, enabling checkpointing, rollback, and branching workflows for AI coding assistants. Create snapshots with auto-generated or explicit names (`coi snapshot create [name]`), list snapshots in text or JSON format (`coi snapshot list`), restore containers from snapshots with confirmation prompts (`coi snapshot restore <name>`), delete individual or all snapshots (`coi snapshot delete <name>`), and show detailed snapshot information (`coi snapshot info <name>`). Supports stateful snapshots (including process memory with `--stateful` flag). Auto-resolves container from workspace or accepts explicit `--container` flag. Restore requires stopped container for safety. Destructive operations require confirmation unless `--force` flag is used. Snapshots are Incus-native and capture complete container state including session data. (#72)

### Enhancements

- [Enhancement] **macOS/Colima documentation and UX improvements** - Updated README with clearer instructions for running COI on macOS via Colima/Lima VMs. Added explicit guidance that `--network=open` is required since Colima/Lima VMs don't include firewalld by default. Documented how to set open network mode as default in config file. Added more detailed setup steps including Colima VM resource allocation and complete installation flow inside the VM. Added warning message when running in open mode without firewalld available to inform users about lack of network isolation.
- [Enhancement] **Update Claude CLI installation to native method** - Replaced deprecated npm installation (`npm install -g @anthropic-ai/claude-code`) with the official native installer (`curl -fsSL https://claude.ai/install.sh | bash`). Anthropic moved away from npm releases as of 2025, making the native installation method the recommended approach. The installer runs as the `code` user and installs to `~/.local/bin/claude` with a global symlink at `/usr/local/bin/claude`. Added verification to ensure the binary exists before creating symlink, preventing broken installations. Users must rebuild the base image with `coi build --force` to get the updated installation method. (#82)
### Technical Details

Firewalld network isolation:
- **Architecture**: Container traffic flows through host's FORWARD chain, firewalld direct rules filter by source IP (container)
- **Container IP Detection**: Uses `incus list --format=json` to get container's eth0 IPv4 address at runtime
- **Rule Priorities**: Priority 0 for gateway allow, 1 for allowlist allows, 10 for RFC1918/metadata blocks, 99 for default deny (allowlist mode)
- **Restricted Mode**: Allows gateway, blocks RFC1918 (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) and metadata (169.254.0.0/16), allows all else
- **Allowlist Mode**: Allows gateway, allows specific IPs from resolved domains, blocks RFC1918 and metadata, default deny all else
- **Cleanup**: Rules are removed when container stops/deletes using container IP as identifier
- **Standard Bridge Networks**: Works with standard Incus bridge networks (incusbr0), no special routing needed

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
- **Network mode**: Colima/Lima VMs don't include firewalld, so `--network=open` must be used. The open mode works without firewalld - it skips firewall rule setup entirely and allows unrestricted network access

This prevents the error: `Error: Failed to start device "workspace": Required idmapping abilities not available` when running COI inside Colima/Lima VMs on macOS.

### Testing

- [Testing] Added Docker integration tests - Three test scenarios: (1) verify Docker nesting flags are automatically enabled on container launch, (2) verify Docker actually works inside containers without network namespace errors, (3) verify Docker fails with a clear error when required nesting flags are not enabled (regression test) (tests/docker/ directory).
- [Testing] Added integration tests for `coi persist` command - Five test scenarios covering basic operation, bulk operations, state verification, and error handling (tests/persist/ directory).
- [Testing] Added comprehensive terminal sanitization tests - Unit tests, integration tests with real tmux sessions, and CI end-to-end tests that verify exotic terminal types work correctly in containers.
- [Testing] Added integration tests for IPv4 display in `coi list` - Three test scenarios covering running containers showing IPv4, stopped containers not showing IPv4, and JSON format including the ipv4 field (tests/list/ directory).
- [Testing] Updated network isolation tests - Removed OVN-specific tests, updated all network tests to work with firewalld-based isolation on standard bridge networks.

### CI/CD Improvements

- [CI/CD] **Simplified CI with firewalld** - Removed OVN setup from CI, using standard bridge networking with firewalld for network isolation tests. CI now installs firewalld and configures it for container networking. Test groups run on a single network type (no more OVN/bridge matrix).
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
