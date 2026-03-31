# CHANGELOG

## 0.8.0 (Unreleased)

### Breaking Changes

- [Breaking] **Project config moved from `.coi.toml` to `.coi/config.toml`** — Project-level configuration must now be placed in `.coi/config.toml` instead of `.coi.toml` in the project root. This enables co-locating build scripts and other project assets alongside config in the `.coi/` directory. COI will refuse to start if a `.coi.toml` file is detected, displaying a migration command: `mkdir -p .coi && mv .coi.toml .coi/config.toml`. (#251)

### Bug Fixes

- [Bug Fix] **Dynamic UID mapping for workspace bind mounts** — When the host user's UID differs from the container's code user UID (default 1000), `shift=true` silently fails — files appear owned by the wrong UID inside the container, causing "Permission denied" for the code user. This affected CI runners (UID 1001) and any non-standard host UID. COI now auto-detects the mismatch using `os.Getuid()` and applies `raw.idmap "both <hostUID> <codeUID>"` to explicitly map the host UID to the container code UID, replacing the previous CI-only hardcoded workaround. Includes integration tests that reproduce the `shift=true` bug and validate the `raw.idmap` fix. Fixes #226.
- [Bug Fix] **Detect Incus bridge not in firewalld trusted zone** — When firewalld is active but the Incus bridge (e.g. `incusbr0`) is not in the `trusted` zone, containers fail to get IP addresses, causing "Waiting for network..." timeouts during `coi build` and `coi shell`. Added a new `bridge_firewalld_zone` health check to `coi health` that warns with an actionable fix command, an automatic bridge zone setup in `install.sh` for all firewalld paths (fresh install, start existing, already running), and a diagnostic hint in `coi build` when container IP detection fails. Fixes #220.
- [Bug Fix] **Prefer IPv4 for Claude CLI installation in containers** — The native Claude Code installer uses Bun/Node which resolves AAAA (IPv6) records first. In containers and some network configurations the IPv6 path is non-functional, causing downloads to either time out or return 403. Two mitigations applied: (1) Set IPv4 precedence in `/etc/gai.conf` so `getaddrinfo()` returns A records first. (2) Use `curl -4` flag to force IPv4 for the initial `install.sh` fetch. Ref: anthropics/claude-code#13498. Fixes #224.
- [Bug Fix] **Raw iptables fallback for Docker FORWARD DROP without firewalld** — Users with Docker installed but no firewalld would get stuck on "Waiting for network..." during `coi build` and `coi shell` because Docker sets the iptables FORWARD chain policy to DROP, blocking all container traffic. COI now auto-detects this scenario (`ForwardPolicyIsDrop()` + no firewalld + iptables available) and applies bridge-level ACCEPT rules as a fallback (`iptables -I FORWARD -i <bridge> -j ACCEPT`). The rules are tagged with `--comment coi-bridge-forward` for identification and cleaned up on teardown. Includes a new `docker_forward_policy` health check in `coi health` that proactively warns about the Docker/firewalld interaction, orphan detection/cleanup for stale bridge rules via `coi clean --orphans`, and diagnostic hints in `waitForNetwork` at the 30-second checkpoint. Also refactored duplicate bridge name parsing into shared `GetIncusBridgeName()`. Fixes #83.

### Features

- [Feature] **Build configuration in project config (`[build]` section)** — Define how to build custom images directly in `.coi/config.toml` using a new `[build]` section. Supports `script` (path to a build script, resolved relative to the config file) or inline `commands` (array of shell commands). When `defaults.image` is set to a custom name and `[build]` is configured, `coi build` builds the custom image automatically. `coi shell` and `coi run` auto-detect missing images and trigger the build from config before launching, printing "Image 'X' not found. Building from config...". Supports `base` to specify a base image (default: `coi`), `--force` to rebuild, and script-over-commands precedence when both are set. Image resolution follows: `--image` CLI flag > `defaults.image` from config > `"coi"`. (#251)

- [Feature] **Host timezone inheritance** — Containers now inherit the host's timezone by default instead of starting with UTC. The host timezone is detected automatically (via `/etc/timezone`, `timedatectl`, or `/etc/localtime` symlink) and applied inside the container through both `/etc/localtime` and the `TZ` environment variable. Override per-session with `--timezone <IANA name>` (e.g., `--timezone Europe/Warsaw`) or `--timezone utc`. Configurable globally via `[timezone]` in config with `mode = "host"` (default), `"fixed"`, or `"utc"`. Also surfaced in `coi health` as a timezone detection check. Works with `coi shell`, `coi run`, and persistent containers (timezone is reset on each session start). Fixes #236.
- [Feature] **Security posture health check** — `coi health` now shows a "Security posture" check under CRITICAL that reports whether seccomp and AppArmor isolation are active on the default Incus profile. Reports full isolation (unprivileged + seccomp + AppArmor), seccomp-only (macOS/Lima where AppArmor is unavailable), warns on custom `raw.seccomp`/`raw.apparmor` overrides, and fails when `security.privileged=true` disables all isolation. Includes detailed JSON output with per-feature status.
- [Feature] **Kernel version check (warning)** — `coi shell`, `coi run`, and `coi build` now warn on stderr when the host kernel is below 5.15, which may lack security features needed for safe container isolation. The check skips gracefully on macOS/darwin and on any failure. Also surfaced as a SYSTEM check in `coi health`. (#237)
- [Feature] **Privileged container guard (hard block)** — COI now refuses to start containers when `security.privileged=true` is detected on either the container config or the default Incus profile, which silently defeats all container isolation. Returns an actionable error message with the exact `incus` command to fix it. Checked in `LaunchContainer`, `LaunchContainerPersistent`, and session setup. Also surfaced as a CRITICAL check in `coi health`. (#237)
- [Feature] **Image compression flag** — Added `--compression` flag to `coi build`, `coi build custom`, and `coi image publish` commands. The flag is passed through to `incus publish`, allowing users to control image compression (e.g., `--compression none` for faster iteration by skipping single-threaded compression). Thanks @rominf! Fixes #233. (#234)
- [Feature] **Auto-inject sandbox context into AI tool sessions** — `~/SANDBOX_CONTEXT.md` is now automatically loaded into each AI tool's native context system at session start. **Claude Code**: sandbox context is written to `~/.claude/CLAUDE.md` (auto-read by Claude at every session start); if the host already has a `CLAUDE.md` with user instructions, those are preserved and sandbox context is appended with a `# COI Sandbox Context` separator. Host `CLAUDE.md` is now also included in `EssentialConfigFiles` so user-level instructions are copied into the container. **OpenCode**: the `instructions` field in `opencode.json` is set to reference `~/SANDBOX_CONTEXT.md` so OpenCode loads it automatically. Controlled by a new `auto_context` config option (default: `true`). Opt out with `[tool] auto_context = false`. Two new tool interfaces (`ToolWithAutoContextFile`, `ToolWithAutoContextPath`) follow the existing optional interface pattern. Includes Go unit tests and Python integration tests. Fixes #243. (#247)

- [Feature] **Expanded container toolset with mise-managed runtimes** — Added modern CLI utilities (`fd-find`, `bat`, `tree`), debugging tools (`strace`, `lsof`), database clients (`sqlite3`, `postgresql-client`, `redis-tools`), and `imagemagick` for image processing to the base container image. Python 3 (`python3`, `pip`, `venv`), `pnpm`, `typescript`, and `tsx` are now installed and managed via **mise** (polyglot runtime manager) instead of system packages/global npm, giving users per-project version pinning via `.mise.toml` and easy installation of additional runtimes (`mise use --global go@latest`, `mise use ruby@3`, etc.). Shell activation (`eval "$(mise activate bash)"`) is configured in both `.bashrc` and `.profile` so mise-managed tools are available in all shell sessions, including those started by Claude and opencode. System Node.js (v20 LTS via nodesource) is retained for Claude CLI and core tooling. Includes integration tests verifying all mise tools are accessible in the built image.

- [Feature] **Sandbox context file: GitHub CLI auth and forwarded env vars** — The `~/SANDBOX_CONTEXT.md` injected into containers now dynamically reflects GitHub CLI authentication status (shows "Authenticated via forwarded token" when `GH_TOKEN` or `GITHUB_TOKEN` is among forwarded env vars) and lists all forwarded environment variable names in a dedicated section. Also added a "Runtime Manager (mise)" section documenting pre-installed mise tools, usage examples, and per-project configuration. Updated the system tools list and best practices to reflect the current toolset. These additions help AI coding agents understand what credentials and runtimes are available without trial-and-error discovery. Includes integration tests for GitHub CLI auth rendering and forwarded env var rendering.

- [Feature] **TTL-aware DNS refresh for allowlist mode** — Domain IP refresh now uses actual DNS TTL values instead of a fixed interval. Domains with short TTLs (e.g., CDNs, cloud services) are re-resolved more frequently, preventing allowed domains from becoming unreachable when their IPs rotate. The `refresh_interval_minutes` config now acts as a maximum cap, with the actual refresh interval being the minimum TTL across all resolved domains. Uses `miekg/dns` for raw DNS queries with graceful fallback to the standard resolver. Enforces a 60-second TTL floor to prevent overly aggressive refreshes.
- [Feature] **Environment variable forwarding** — Forward host environment variables into containers by name using `forward_env = ["ANTHROPIC_API_KEY", "GITHUB_TOKEN"]` in the `[defaults]` config section, or per-session with `--forward-env ANTHROPIC_API_KEY,GITHUB_TOKEN`. Values are read from the host at session start and never stored in config files. Also wires up the previously unused `ProfileConfig.Environment` so profile-level env vars (e.g., `environment = { RUST_BACKTRACE = "1" }`) are now applied to the container.
- [Feature] **SSH agent forwarding** — Forward the host's SSH agent into containers so git-over-SSH works without copying private keys. The host's `SSH_AUTH_SOCK` is bridged into the container at `/tmp/ssh-agent.sock` via an Incus proxy device. Enable per-session with `coi shell --ssh-agent` or permanently with `forward_agent = true` under `[ssh]` in config. Disabled by default. Includes automatic retry logic to handle an Incus race condition where proxy devices on freshly-launched containers may not create the listen socket immediately. Includes integration tests for end-to-end forwarding, graceful skip on missing socket, invalid socket path, and device replacement on persistent containers.
- [Feature] **Sandbox context file injection** — Automatically injects a `~/SANDBOX_CONTEXT.md` file into every container during setup, describing the COI sandbox environment (workspace path, network mode, persistence, SSH agent, Docker availability, user privileges, and limitations). The file is rendered from an embedded Go `text/template` at runtime with session-specific values, so AI tools can read it to understand their sandbox constraints. Supports a `context_file` TOML config option under `[tool]` to override with a user-provided `.md` file (supports `~` expansion). The file is regenerated on every session start (including resume), so dynamic info stays current. Includes unit and integration tests.
- [Feature] **Minimum Incus version check (>= 6.1)** — `coi doctor` and `install.sh` now detect old Incus versions (e.g., Ubuntu's 6.0.x packages) that lack required idmapping support, which previously caused the cryptic error *"Failed to setup device mount: idmapping abilities are required"*. Users are shown a clear error with a link to install Incus >= 6.1 from the Zabbly repository. The health check returns `StatusFailed` with a non-zero exit code. Gracefully degrades if the version cannot be parsed. Includes reusable version parsing utilities and unit/integration tests. Fixes #212.
- [Feature] **Minimum nftables version check (>= 0.9.0)** — `coi doctor` now validates the installed nftables version and fails if it is below the minimum required 0.9.0 for network monitoring features. The nft version is also displayed in the health check output. Includes reusable version parsing utilities and unit/integration tests.
- [Feature] **Split version checks between health (warn) and CLI commands (fail)** — `coi health` now reports old Incus/nftables versions as warnings (degraded, not unhealthy), while `coi shell`, `coi run`, and `coi build` fail with a clear error if Incus < 6.1. Added `container.CheckMinimumVersion()` helper. (#214)
- [Feature] **Improved firewalld setup UX with masquerade checks and auto-setup** — `coi health` now reports granular firewall state (installed/running/masquerade) with actionable fix commands. `install.sh` offers interactive auto-setup for firewalld (install, start, enable masquerade, configure passwordless sudo). (#216)
- [Fix] **install.sh works with curl|bash pipe** — Fixed `read` consuming script content from stdin instead of user input when piped. Added `/dev/tty` helpers and non-interactive mode auto-detection. (#215)
- [Fix] **Post-install firewalld hint updated for auto-setup flow** — Updated `post_install()` hint to mention masquerade and align with the new interactive setup from #216. (#217)
- [Fix] **install.sh interactive detection fixed for curl|bash** — `curl -fsSL ... | bash` incorrectly entered non-interactive mode because `[ -t 0 ]` is always false when stdin is a pipe. Replaced with a `/dev/tty` open check (`true </dev/tty`) which correctly detects whether a controlling terminal is available, since all `read` calls already redirect from `/dev/tty`. Thanks @dgrant! (#222)

## 0.7.0 (2026-03-10)

### Bug Fixes

- [Fix] **opencode session resume in ephemeral mode** — `coi shell --tool=opencode --continue` now correctly resumes the previous session. Two fixes: (1) `BuildCommand()` passes `--continue` (or `--session <id>`) to opencode when resuming, instead of always returning bare `opencode`. (2) New `ToolWithContainerEnv` interface allows tools to inject environment variables into the container; opencode uses this to set `XDG_DATA_HOME` and `XDG_STATE_HOME` to workspace subdirectories, so the SQLite database persists on the host mount across ephemeral container recreations instead of being destroyed with the container's `~/.local/share/opencode/`. Fixes #196.
- [Fix] **`coi build --force` works from any directory** — Build assets (`coi.sh` build script and `dummy` test stub) are now embedded into the binary at compile time via `//go:embed`, so `coi build` no longer requires running from the project root. Disk files still take priority for development (#176).
- [Bug Fix] **Docker Compose fails with sysctl permission denied in containers** — Newer runc versions (1.3.x) try to write `net.ipv4.ip_unprivileged_port_start` via a detached procfs mount during container init, which AppArmor blocks in nested containers. Added `linux.sysctl.net.ipv4.ip_unprivileged_port_start=0` to `EnableDockerSupport()` so the value is pre-set at the Incus level before the container boots, preventing the permission denied error. Fixes #187.
- [Bug Fix] **Persistent session resume creates fresh container instead of reusing stopped one** — When resuming a persistent session with `--resume`, slot allocation skipped the original stopped container (because it was occupied) and allocated a new slot, creating a fresh container. System-level changes (installed packages, files outside config dirs) were lost. Fixed by extracting the original slot from session metadata and reusing it on resume, so the stopped container is restarted instead of replaced. Fixes #190.
- [Bug Fix] **Opencode session resume broken due to hardcoded `.claude` check** — `SessionExists()` and `ListSavedSessions()` hardcoded `.claude` as the directory to check when detecting saved sessions. Opencode sessions save config under `.config/opencode`, so the lookup never found them, breaking both `--resume` and `--resume=<uuid>`. Changed to check for `metadata.json` instead — a tool-agnostic indicator present in every saved session. Fixes #183.
- [Bug Fix] **Opencode interactive permission mode ineffective** — When `permission_mode = "interactive"`, `GetSandboxSettings()` returned an empty map. In opencode, no permissions config means no restrictions, making interactive mode identical to bypass mode. Fixed by returning `{"permission": {"*": "ask"}}` in interactive mode. Fixes #186.
- [Bug Fix] **Docker Compose fails inside session containers** - Fixed Docker/nested container support flags (`security.nesting`, `security.syscalls.intercept.mknod`, `security.syscalls.intercept.setxattr`) not being set on session containers created via `coi shell`. The main session setup path (`session/setup.go`) used `incus init` + `incus start` but never called `enableDockerSupport()`, so containers launched via the primary user-facing flow had no Docker support. Also changed `LaunchContainer`/`LaunchContainerPersistent` to set security flags before first boot (using `incus init` + configure + `incus start` instead of `incus launch` + configure) to eliminate a race condition. Added Docker Compose integration test.
- [Bug Fix] **Fix double-cleanup race condition in shell signal handler** - When a signal (e.g., SIGINT/SIGTERM) arrived while `shellCommand` was already returning normally, both the `defer` and the signal handler goroutine could call `doCleanup()` concurrently — stopping monitoring daemons twice and running session cleanup twice. Wrapped `doCleanup` in `sync.Once` to ensure cleanup executes exactly once regardless of which path triggers first. Includes integration test.
- [Bug Fix] **Container user UID/GID remapping for non-default code_uid** - When `code_uid` is set to a value other than 1000, COI now remaps the container user's UID/GID to match. Previously, the container user stayed at UID 1000 (baked into the image), causing "Permission denied" on `.bashrc`, "I have no name!" prompts, and group lookup failures. Fixes #166.
- [Bug Fix] **Resume path now uses ToolWithConfigDirFiles interface** - Fixed `injectCredentials()` hardcoding Claude-specific assumptions (requiring `.credentials.json` and using `.${tool}.json` state filename). The resume path now uses `EssentialConfigFiles()`, `StateConfigFileName()`, and `SandboxSettingsFileName()` from the interface, so tools like opencode that don't use credential files can resume without errors.
- [Bug Fix] **opencode global config now uses correct XDG location** - Fixed opencode configuration being placed at `~/.opencode.json` (old format) instead of `~/.config/opencode/opencode.json` (current XDG-standard location). The sandbox permission bypass and user's global config are now correctly injected. Also copies `tui.json` when present. Fixes #158.
- [Bug Fix] **opencode install URL updated** - Fixed the opencode installation script using an outdated GitHub raw URL that installed an old version (v0.0.55). Updated to the official `https://opencode.ai/install` URL per the opencode docs. Fixes #157.
- [Bug Fix] **NFT monitor daemon errors no longer corrupt TUI** - Fixed `nftmonitor.Daemon` using `fmt.Printf` to report errors directly to stdout, which corrupted the terminal UI. Errors from log reader failures and threat handling now route through an `OnError` callback (matching the pattern already used by `monitor.Daemon`). Added `OnError func(error)` to `nftmonitor.Config` and wired it in the CLI. Includes regression tests.
- [Bug Fix] **Config merge no longer silently drops boolean settings** - Fixed multi-layer config merge (`/etc/coi/config.toml` → `~/.config/coi/config.toml` → `.coi.toml`) silently overriding boolean fields with `false` when the higher-priority file omitted them. Security-critical defaults like `block_private_networks`, `block_metadata_endpoint`, `auto_pause_on_high`, `auto_kill_on_critical`, and `auto_stop` could all be lost. Converted 13 boolean config fields to pointer types (`*bool`) so `nil` correctly represents "not set in this file", extending the pattern already used by `git.writable_hooks`. Includes regression test.
- [Bug Fix] **Incus config values now applied to command execution** - Fixed `incus.project`, `incus.group`, `incus.code_uid`, and `incus.code_user` config settings being ignored. These values were defined as hardcoded constants in the container package while the config struct had matching fields that were never wired in. The constants are now package-level variables initialized from the loaded config via `container.Configure()`, so custom TOML settings (e.g., `incus.project = "myproject"`) take effect on all Incus command execution.
- [Bug Fix] **Settings.json merge now preserves user env vars** - Fixed sandbox settings merge overwriting user's `env` section in `settings.json`. The shallow `dict.update()` replaced the entire `env` dict, losing user-configured environment variables (e.g., AWS Bedrock settings like `AWS_PROFILE`). Changed to deep merge so nested dicts like `env` are merged key-by-key instead of replaced wholesale.
- [Bug Fix] **`--monitor` flag now enables NFT network monitoring** - Fixed `coi shell --monitor` only enabling traditional process/filesystem monitoring while leaving `cfg.Monitoring.NFT.Enabled` as `false`, so the NFT monitoring daemon never started and NFT rules were never created or cleaned up. Also added `DetectOrphanedNFTMonitorRules()` and `CleanupOrphanedNFTMonitorRules()` to `coi clean --orphans` so orphaned NFT rules (from containers that were removed without cleanup) are now detected and removed.
- [Bug Fix] **Hardcoded `/workspace` paths resolved for `preserve_workspace_path`** - Fixed `coi attach`, `coi run`, and `coi container exec` using hardcoded `/workspace` instead of the dynamic workspace path when `preserve_workspace_path` was enabled. Added `GetWorkspacePath()` to auto-detect the workspace mount path from container device configuration. Also added system directory validation to prevent mounting workspace over critical paths. Includes integration tests.
- [Bug Fix] **Flaky `test_high_threat_without_auto_pause` test stabilized** - Fixed intermittent test failure caused by unreliable I/O patterns: a borderline 50MB text file read via Python (page-cached reads may not trigger cgroup I/O counters). Replaced with a 200MB binary file read via `dd` with `O_DIRECT` (bypasses page cache), added `file_read_rate_mb_per_sec` config, and added pre-state verification with debug output on unexpected states.
- [Bug Fix] **Protected paths work with preserve_workspace_path** - Fixed protected paths (`.git/hooks`, `.vscode`, etc.) not being mounted at the correct location when `preserve_workspace_path` was enabled. The security mounts were hardcoded to use `/workspace` instead of the dynamic container workspace path. Also added validation to prevent mounting workspace over critical system directories (`/etc`, `/bin`, `/usr`, etc.) and updated all CLI code paths to use the dynamic workspace path. Includes integration tests for protected paths with `preserve_workspace_path`.
- [Bug Fix] **Large write detection test timeout handling** - Fixed the large write detection integration test timing out on slow CI systems. The test now handles `TimeoutExpired` exceptions gracefully and still verifies threat detection by checking audit logs. Reduced subprocess timeout and added proper cleanup on timeout scenarios.
- [Bug Fix] **NFT monitoring rules now cleaned up on container kill** - Fixed NFT monitoring rules not being removed when containers are killed via `coi kill` or auto-killed by the security responder (e.g., when metadata endpoint access is detected). Added `CleanupNFTMonitoringRules()` to the network package and integrated it into both kill paths: `coi kill` command and the responder's `killContainer()` method. The cleanup gracefully handles cases where no rules exist (monitoring wasn't enabled) or the nftables chain doesn't exist.
- [Bug Fix] **Complete cleanup on all container termination paths** - Fixed two edge cases where network rules were not properly cleaned up: (1) `coi shutdown` was missing NFT monitoring rules cleanup (only firewall rules were cleaned). (2) The security responder's auto-kill was missing firewall rules cleanup (only NFT rules were cleaned). Both termination paths now consistently clean up both firewall rules and NFT monitoring rules. Includes integration tests for both scenarios.
- [Bug Fix] **Veth zone binding cleanup on responder auto-kill** - Fixed firewalld zone bindings not being cleaned up when the security responder auto-kills a container (e.g., when metadata endpoint access is detected). The responder now gets the veth interface name before stopping the container and removes it from firewalld zones after deletion, matching the behavior of `coi kill` and `coi shutdown`. Includes integration test.
- [Bug Fix] **Security monitoring no longer spams alerts or corrupts TUI** - Fixed multiple issues with security monitoring: (1) The responder now tracks pause/kill state to prevent infinite retry loops when container is already frozen. (2) Added threat deduplication with 30-second window to prevent alert spam. (3) Removed all debug log statements that corrupted the coding agent TUI. (4) All monitoring output now goes to audit log file only. (5) Added `OnAction` callback that only prints to stderr when container is actually paused or killed - the only output users will see. (6) Added double-check filter in nftables log reader to ensure source IP matches container IP. (7) Added `IncusOutputWithStderr` function to properly capture Incus error messages like "already frozen" which are written to stderr. (8) Added comprehensive integration tests for responder and resume command.
- [Bug Fix] **Firewall rule accumulation causing system hang** - Fixed multiple issues causing firewall rules to accumulate to 100k+ entries, eventually hanging the system: (1) Signal handler used `os.Exit()` which skips deferred cleanup - now explicitly calls cleanup before exit. (2) `coi shutdown` was missing firewall cleanup (only `coi kill` had it). (3) `coi clean --orphans` was reloading firewalld after removing zone bindings, which wiped Docker's dynamically-added nft rules - removed the unnecessary reload. (4) RFC1918 addresses were incorrectly flagged as suspicious in "open" network mode (no restrictions), causing false-positive container freezes from host traffic like Synology Drive. Includes comprehensive integration tests for firewall cleanup verification across all network modes and cleanup scenarios.
- [Bug Fix] **Docker commands now work without sudo for the code user** - The `code` user was correctly added to the `docker` supplementary group, but `incus exec --group` may not call `initgroups()` to load supplementary groups depending on the Incus version/configuration, leaving the docker socket inaccessible without `sudo`. Fixed with two layers: (1) `/etc/docker/daemon.json` sets `"group": "code"` so Docker chowns the socket to `root:code` on startup; (2) a `docker.socket` systemd drop-in sets `SocketGroup=code` so Ubuntu's socket-activation path also creates the socket with the correct group. The `code` user's primary group (GID 1000) is always active regardless of how the incus session was started, so `docker` works without `sudo` in all session types. Adds a test that explicitly runs `docker run` as UID 1000 / GID 1000 (replicating the real-world `incus exec --user 1000 --group 1000` call) to catch regression. Fixes #134.
- [Bug Fix] **Fix /tmp exhaustion causing agents to hang silently** - Coding agents that run heavy builds (npm, cargo, Docker) could fill `/tmp` and freeze indefinitely with no error. `/tmp` is now backed by the container's virtual root disk by default (no RAM cap, shares the storage pool allocation) instead of an artificially small RAM-backed tmpfs. The previous `SetTmpfsSize()` implementation was also corrected — it was setting `limits.memory.tmpfs`, an invalid Incus config key that was silently dropped; it now uses the correct `size=` device property. The RAM-backed tmpfs mode remains available as opt-in via `tmpfs_size = "4GiB"` under `[limits.disk]`. Config merging now correctly propagates `tmpfs_size` overrides. The base image ships `/etc/tmpfiles.d/coi-tmp-cleanup.conf` (1-hour age threshold) and a `systemd-tmpfiles-clean.timer` override (15-minute interval) so abandoned artefacts are reclaimed automatically. Includes unit and integration tests. Wiki Troubleshooting and FAQ pages updated. Fixes #135.
- [Bug Fix] **Automatic cleanup of orphaned firewalld zone bindings** - Added detection and cleanup of orphaned veth interface zone bindings in firewalld that accumulate when containers are deleted without proper cleanup. COI now automatically removes veth zone bindings during container deletion (via `coi kill` and `coi clean`) and provides `coi clean --orphans` to detect and remove orphaned resources including: veth interfaces (no master bridge), firewall rules (non-existent container IPs), and firewalld zone bindings (veths in zones but not on system). This prevents firewalld from accumulating stale interface bindings over time, which could cause configuration bloat and potential conflicts. Includes integration tests for firewall cleanup functionality. (#130)
- [Bug Fix] **Fix firewall rule cleanup on container delete** - The `coi container delete` command was missing firewall rule cleanup, causing rule accumulation when containers were deleted (especially during test cleanup). Now properly gets container IP before deletion and removes all firewall rules for that IP. This completes the firewall cleanup fixes ensuring all container deletion paths (`coi kill`, `coi container delete`, `session.Cleanup()`) properly clean up firewall rules. Fixes #119. (#120)
- [Bug Fix] **Fix flaky installation smoke test** - Fixed `test_full_installation_process` failing intermittently when trying to clone PR branches that don't exist in the main repository. This occurred for fork PRs (branch only exists in fork) or after branches were deleted post-merge. The test now tries the specific branch first and falls back to the default branch if unavailable. (#121)
- [Bug Fix] **Fix flaky allowlist network blocking tests** - Fixed `test_allowlist_blocks_non_allowed_dns` failing intermittently because it ran immediately after container startup without waiting for firewalld rules to fully propagate. In CI environments, firewalld rule propagation can be slow, causing traffic to 9.9.9.9 (Quad9 DNS) to succeed when it should be blocked. Added `time.sleep(5)` before all blocking tests and increased existing sleeps from 2s to 5s for consistency. (#122)
- [Bug Fix] **Handle cross-device link and symlinks when saving session data** - Fixed `EXDEV` (cross-device link) error when `/tmp` and session directory are on different filesystems. `PullDirectory` now falls back to recursive copy when `os.Rename()` fails with cross-device error. Added proper symlink handling - symlinks (e.g., `.claude/debug/latest`) are now correctly detected and recreated instead of being treated as regular files. This prevents session save failures on systems where temp storage and session storage are on different mount points. Thanks @psaab! (#106)
- [Bug Fix] **Increased test timeout values for CI reliability** - Comprehensively increased timeouts across all ephemeral shell tests to improve CI reliability. Container deletion timeout increased from 30s to 90s, container operations from 30s to 90s, network teardown from 60s to 120s, and other operations from 30s to 90s. CI environments need significantly more time for container cleanup after poweroff, container deletion operations, and network teardown operations. This fixes all timing-related test failures in shell-ephemeral tests.

### Features

- [Feature] **Configurable permission mode** — New `permission_mode` option under `[tool]` config allows switching between `"bypass"` (default, current behavior with all permissions auto-granted) and `"interactive"` (human-in-the-loop, tool asks before running commands). Supports both Claude and opencode. Configure via `permission_mode = "interactive"` in your `.coi.toml` under `[tool]`.
- [Feature] **Preserve workspace path option** - Added `preserve_workspace_path` config option that mounts the workspace at the same absolute path inside the container as on the host, instead of `/workspace`. This is useful for tools like opencode that store session data relative to the workspace directory, allowing sessions to persist correctly when the same project is opened from different machines or after container recreation. Configure with `[paths] preserve_workspace_path = true` in `~/.config/coi/config.toml` or `.coi.toml`. Off by default. Fixes #108.
- [Feature] **Large write detection for data exfiltration prevention** - Added detection for large filesystem writes as a potential data exfiltration vector. When write activity exceeds the threshold (default: same as read threshold, 50MB), a HIGH-level threat is triggered. This complements existing read monitoring to catch scenarios where an attacker uses `tar`, `dd`, or similar tools to package and exfiltrate data. Configurable via `file_write_threshold_mb` and `file_write_rate_mb_per_sec` in monitoring config. Includes integration tests for write detection and no-alert scenarios.
- [Feature] **Disk space monitoring** - The monitoring system detects when `/tmp` exceeds 80% usage and triggers a WARNING threat. This protects against runaway builds that could fill tmpfs and cause container hangs. The detection logic is verified via Go unit tests in `internal/monitor/detector_test.go`. Note: Integration tests for this feature were removed as they require a small tmpfs (<500MB) which cannot be configured in CI due to base image limitations.
- [Feature] **Concurrent threat detection tests** - Added integration tests for concurrent threat scenarios: (1) simultaneous reverse shell + environment scanning detection, (2) rapid threat burst handling with deduplication. These tests verify the monitoring system correctly handles multiple threats in the same monitoring cycle.
- [Feature] **Claude effort level configuration** - Added support for configuring Claude Code's effort level to prevent interactive prompts during autonomous sessions. Configure with `[tool.claude] effort_level = "medium"` in `.coi.toml`. Valid values: `"low"`, `"medium"` (default), `"high"`. Implementation adds `ClaudeToolConfig` nested struct, a `ToolWithEffortLevel` interface, and injection of `effortLevel` into `settings.json`. Includes unit tests.
- [Feature] **NFT monitor reports dropped events** - The NFT monitor `LogReader` now tracks dropped network events with an atomic counter and reports via the `OnError` callback on the 1st drop and every 100th thereafter, avoiding log flooding during sustained bursts while ensuring operator visibility into event loss. Also increased `eventChan` and `journalChan` buffer sizes from 100 to 1000 (~120KB) for better burst absorption during high-frequency network activity. Includes unit test.
- [Feature] **`coi resume` command** - Added command to resume containers paused by the security monitoring system. Use `coi resume <container-name>` to resume a specific frozen container, or `coi resume` to resume all frozen COI containers. Only works with containers that are in the Frozen state.
- [Feature] **`--tool` flag for `coi shell`** - Added `--tool` flag to override the configured AI tool for a single session without creating or modifying `.coi.toml`. Example: `coi shell --tool opencode`. Takes precedence over `tool.name` in config files.
- [Feature] **Incus storage pool health check** - Added `incus_storage_pool` check to `coi health` that reports used/free/total space in the Incus storage pool (shared by all images, containers, and snapshots). Detects the pool name from the default Incus profile automatically. Warns when free space drops below 5 GiB or usage exceeds 80%, fails below 2 GiB or above 90%. Includes 2 integration tests.
- [Feature] **opencode support** - Added opencode (https://opencode.ai) as a supported AI coding tool. opencode is installed in the default base image alongside Claude CLI. Configure it with `[tool] name = "opencode"` in `.coi.toml` or use `coi shell --tool opencode`. COI automatically copies `~/.opencode.json` from the host (if present) into the container and merges the permission bypass config (`"permission": {"*": "allow"}`) so opencode runs without interactive permission prompts. **Note:** Unlike Claude (which shows a login screen), opencode requires API keys to start — set `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` environment variable, or configure providers in `~/.opencode.json`. Resume support uses `--continue` to resume the last SQLite session. Implements the new `ToolWithHomeConfigFile` optional interface for tools that use a single home-dir JSON file rather than a config subdirectory. Includes 9 unit tests and 2 integration tests. Fixes #117.
- [Feature] **Base image includes ripgrep and fzf** - Added `ripgrep` (rg) and `fzf` to the default base image dependencies. These are used by opencode and other tools for fast file searching and fuzzy finding.
- [Feature] **nftables-based network monitoring** - Added event-driven network monitoring using nftables and systemd-journal for complete visibility into container network activity. Replaces polling-based /proc/net/tcp monitoring with kernel-level packet filtering that captures all network events including short-lived connections (<2s), blocked connection attempts, and DNS queries. Features real-time threat detection for RFC1918 addresses, metadata endpoint access (169.254.169.254), suspicious ports (4444, 5555, 31337), allowlist violations, and DNS query anomalies. Monitoring daemon runs automatically during sessions when `[monitoring.nft]` is enabled (default: true). Logs are streamed from journald with configurable rate limiting (default: 100 packets/second for normal traffic, unlimited for suspicious traffic). Includes health checks for nftables, systemd-journal access, and libsystemd-dev installation. Setup script provided at `scripts/install-nft-deps.sh` to configure system dependencies, group membership, and passwordless sudo for nft commands. Works on Lima/macOS via limactl wrapper. Audit logs stored at `~/.coi/audit/<container-name>-nft.jsonl`. This provides kernel-level security monitoring that can't be tampered with from inside the container, catching threats that process-level monitoring might miss.
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

### Refactoring

- [Refactoring] **Unified config handling for all tools** - Both `ClaudeTool` and `OpencodeTool` now implement the `ToolWithConfigDirFiles` interface, eliminating hardcoded Claude-specific defaults from `setupCLIConfig()`. Removed the dead `ToolWithHomeConfigFile` interface and `setupHomeConfigFile()` function. The `ToolWithConfigDirFiles` interface now includes `StateConfigFileName()` and `AlwaysSetupConfig()` so each tool fully describes its own config layout.
- [Refactoring] **Decompose shell.go duplicated code** - Extracted three helper functions (`buildCLICommand`, `buildContainerEnv`, `ensureTmuxServer`) from `runCLI()` and `runCLIInTmux()` to eliminate ~76 lines of duplicated code. Also removed a redundant second tmux server-polling loop in the interactive branch of `runCLIInTmux()`. Pure refactoring with no behavioral changes.
- [Refactoring] **Replace Evidence `interface{}` with typed structs** - Both `monitor.ThreatEvent` and `nftmonitor.ThreatEvent` used `Evidence interface{}` to carry threat-specific data, losing type safety. Replaced with a typed `Evidence` struct (optional fields pattern) in the monitor package and a concrete `*NetworkEvent` type in nftmonitor. Also fixes a bug where the responder's deduplication key enrichment (`responder.go:63`) used a duck-type assertion for `String()` that never succeeded because no evidence types implemented it. Added `DiskSpaceInfo` struct to replace an ad-hoc `map[string]interface{}` for disk space warnings.
- [Refactoring] **Add `context.Context` support to Incus command execution** - Added `*Context` variants for all `IncusExec*`, `IncusOutput*`, and `IncusFilePush` functions; originals become thin `context.Background()` wrappers (non-breaking). Context is wired through the monitoring daemon's collector and responder call chains so `Daemon.Stop()` can now interrupt in-flight Incus subprocesses instead of hitting a 5-second shutdown timeout. Also wired context through the NFT monitor daemon's responder call. Deleted dead code: `ContainerExec` and `ContainerExecOptions` (zero callers). Includes integration tests verifying context cancellation terminates long-running commands promptly.

### Documentation

- [Documentation] **Image management operations** - Added inline examples to README for existing `coi image` commands (list, publish, exists, delete, cleanup). These commands have been available since early releases for creating and managing custom container images, with 20 integration tests and complete wiki documentation. Quick reference examples now included in README for common operations like publishing containers as images, checking image existence, and cleaning up old image versions. See [Image Management wiki page](https://github.com/mensfeld/code-on-incus/wiki/Image-Management) for detailed usage examples and workflows.
- [Documentation] **File transfer operations** - Documented existing `coi file push` and `coi file pull` commands in README. These commands have been available since early releases for transferring files and directories between host and containers, supporting recursive operations with the `-r` flag. Includes 16 integration tests covering various scenarios. See [File Transfer wiki page](https://github.com/mensfeld/code-on-incus/wiki/File-Transfer) for detailed usage examples.
- [Documentation] **nftables monitoring setup** - Added comprehensive documentation for nftables-based network monitoring including system requirements, setup instructions, configuration options, and verification steps. Includes inline examples in README for installing dependencies (libsystemd-dev, nftables), configuring group membership and passwordless sudo, and verifying the setup with `coi health`. Documents the architecture, threat detection capabilities, and audit logging format.

### CI/CD Improvements

- [CI/CD] **Zabbly package install fallback to Ubuntu native Incus** - CI no longer fails when `pkgs.zabbly.com` is unreachable or its packages can't be downloaded. If the Zabbly repo install fails, the CI automatically removes the Zabbly sources and falls back to Ubuntu 24.04's native `incus` package. This eliminates CI being blocked by third-party infrastructure outages.
- [CI/CD] **Base Ubuntu image caching** - The `ubuntu-22.04` base image used for bind mount tests and COI image builds is now cached in GitHub Actions cache (same pattern as the COI image). Subsequent CI runs restore from cache without any dependency on `images.linuxcontainers.org`.
- [CI/CD] **Bind mount test made non-blocking** - The bind mount functionality test step now uses `continue-on-error: true` and skips gracefully when the base image is unavailable. This diagnostic step no longer gates the actual test suite.
- [CI/CD] **NFT rule assertion stabilized** - Replaced fixed `sleep(10)` followed by immediate assertion with a polling loop (up to 30s, 2s intervals) for NFT rules to appear, fixing intermittent `test_nft_rules_cleaned_on_coi_shutdown` and `test_rules_cleanup_on_session_end` failures caused by variable monitoring daemon startup time.

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
