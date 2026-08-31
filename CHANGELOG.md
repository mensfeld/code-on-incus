# CHANGELOG

## 0.12.0 (Unreleased)

### New Features

- **Tool-agnostic launch spec: `coi tool spec` (#751)** — a **non-executing** command that prints the exact launch **command + env** for the profile's tool inside an existing container, so an external orchestrator can drive **any** coi-supported tool without re-implementing its CLI. `coi tool spec --container <ctr> --session-id <id> --prompt-file <host> [--system-prompt-file <host>] [--continue[=<id>]] --json` stages the prompt(s) into the container and emits `{"command": [...argv...], "env": {…}}`: `command` is the tool's argv built from `tool.Tool` (session id, resume, model, permission) — correct for any tool (claude/codex/…), with the prompt embedded via a staged-file `"$(cat …)"` reference so arbitrary text (quotes/newlines) can't corrupt it; `env` is **tool-derived only** (model/effort). Tools that can't embed the prompt in argv (e.g. opencode) instead get a `prompt` field — the in-container path to the staged prompt file — for the orchestrator to deliver out-of-band after launch. Secrets/auth stay with the caller, which adds its own `--env` when it execs the command through **its own** container exec + tmux — keeping direct streaming, input, and lifecycle. `--continue[=<id>]` resolves continue-or-fresh via `DiscoverSessionID`. The caller-supplied `--session-id` (and `--continue` id) are validated as safe tokens up front, since they become a filename component and are joined into the shell-run command. Reuses the `ToolWithPrompt` capability interface (implemented by Claude and Codex; tools without it surface the prompt via the `prompt` field for out-of-band delivery). The general `coi tmux send` / `coi tmux capture` utilities compose with it for out-of-band prompt delivery. Note: the per-tool prompt/system-prompt flags are modeled on each CLI's documented shape and should be validated against the tool versions you run.

- **`coi top` — per-container (and per-process) resource usage (#707)** — shows live CPU%, memory, disk I/O, and network I/O for your running containers, sorted busiest-first and resolved to each container's alias + workspace, so you can tell which container is loading your machine without mapping PIDs by hand. `coi top <name|alias>` (or `coi top --procs` across all containers) drills into per-process rows showing the **host-side PID**, so a runaway is directly `sudo kill`-able. CPU%/IO are sampled over a short `--interval` (default 2s); `--sort cpu|mem|disk|net`, `--json`, and `--watch N` (re-render every N seconds until Ctrl+C, like `coi monitor --watch`) are supported. Reuses the existing cgroup/`/proc` collectors from the monitor subsystem. The per-container row reads the container's **top-level** cgroup so its memory/CPU/I/O aggregate the whole process tree — not just the container's `init` process — matching what the per-process view sums. On Incus/LXC layouts that split an instance into separate monitor and payload cgroups, the collectors now probe the **payload** cgroup (the container's process tree) before the monitor cgroup (the host-side forkstart), so resource stats and init-PID resolution target the right one there too.

- **Machine-readable `~/SANDBOX_CONTEXT.json` (#705)** — a structured, versioned companion to `SANDBOX_CONTEXT.md` for programmatic consumers. Toggle with `[tool] context_json`, override with `context_json_file`. Custom context files (`context_file`/`context_json_file`) are now honored only from trusted config.

- **OpenAI Codex CLI is now a supported tool (#698, thanks @breml)** — `[tool] name = "codex"` launches straight into codex like the other tools, with per-tool `model`/`reasoning_effort`. Opt it into the image with `[container.build] agents = ["claude", "codex"]`.

- **Oh My Pi (`omp`) is now a supported tool (#743, thanks @VIVAAN-DHAWAN)** — `[tool] name = "omp"` launches straight into [omp](https://github.com/can1357/oh-my-pi) like the other tools. Following the `pi` integration, session data is redirected to the workspace mount (`OMP_SESSION_DIR`) to survive ephemeral container recreation, and the sandbox context is wired into omp's config dir (`~/.omp/APPEND_SYSTEM.md`). Opt it into the image with `[container.build] agents = ["claude", "omp"]`. Note: the session-dir and system-prompt mechanisms are modeled on `pi`'s and should be validated against the omp version you run.

- **Per-host ports on `[[network.hosts]]`** — scope a single LAN service to specific ports (`ports = [443]`) while the rest of the internet stays open; also available at runtime via `coi hosts add … --ports`.

- **`[git] readonly = true`** — lock the container's git commit identity read-only so the agent can't overwrite who commits are authored by.

- **Per-destination ports in `allowed_domains` (#704)** — allowlist entries accept a `:ports` suffix (`"github.com:443"`, `"10.0.0.0/8:22"`), so each destination is reachable only on its own ports.

- **`[network] dns_servers` and `allowed_ports` (#704)** — pin which DNS resolvers are reachable and cap outbound destination ports, tightening egress beyond the active network mode.

### Changed

- **Trimmed the injected `SANDBOX_CONTEXT.md` ~30% to save per-session tokens (#718)** — the sandbox context prepended to the agent's instructions every session dropped from ~2,260 to ~1,570 tokens (SSH+GitHub case) by removing sections that only restated others: `What You Can Do` and `Best Practices` duplicated the autonomy, mise, and file-ownership guidance already present elsewhere. All distinct instructions are preserved (a second, deliberate autonomy reminder is kept), and the triplicated git-identity block is now a set of shared template blocks so the three auth variants no longer drift. No behavior or config changes.

### Fixed

- **Profile `[tool.*]` config (model, effort) now reaches an external `coi container exec`, and applies on reused containers (#744)** — a profile's `[tool.claude] model` / `effort_level` were delivered only into the container's `~/.claude/settings.json`, which is written **only on fresh setup** (`setupCLIConfig` is gated on new-container creation). An orchestrator that prepares a container with `coi shell --background --profile <name>` and then runs the tool through a separate `coi container exec` (e.g. coipond) got none of it, and a **reused/persistent** container never had the config (re)applied at all — so per-workflow model selection silently fell back to the tool's default. coi now also persists the tool's resolved env as **container-level `environment.*` Incus config** on every setup (fresh and reused), which Incus injects into every exec, so any launcher inherits it. Stale keys from a previous profile are reconciled (unset) via a `user.coi.tool_env_keys` marker. `ClaudeTool` now exposes model/effort via `GetContainerEnv` so this is tool-agnostic (opencode/pi env rides the same path). Values are validated (no control chars) before being set. Covered by unit tests (reconciliation/validation) and an end-to-end test that `printenv`s the model through `coi container exec`.

- **Security monitor no longer false-freezes a container on a transient I/O-counter blip** — the filesystem monitor computes per-interval read/write amounts as an **unsigned** delta of the container's cumulative cgroup I/O counters. When `CollectResourceStats` hit its io-error branch (a momentarily unreadable `io.stat` returns `IOReadMB/IOWriteMB = 0` while the rest of the sample stays valid), the next interval's `current - previous` **underflowed to ~2^64 bytes**, producing a spurious multi-terabyte "read" that instantly crossed the large-read threshold and auto-paused/froze a perfectly healthy container. The delta is now clamped to 0 whenever the counter goes backwards (transient zero, or a counter reset on container restart), mirroring the negative-delta clamp already used for the `coi top` rate counters. Covered by a unit test.

- **Ephemeral launch retry no longer misattributes start failures to UID isolation, and keeps the `disable_shift` hint (#716)** — when Incus deleted a failed ephemeral container and coi recreated it without `security.idmap.isolated`, it always printed "UID namespace isolation not supported in this environment" regardless of the real cause, and returned the recreate error raw — dropping the `disable_shift` hint that the other retry paths give for the `#678` idmapped-mount failure class. The message is now a neutral "recreating without UID namespace isolation and retrying", and an idmapped-mount failure routes through the `disable_shift` hint like the other paths. The recreate's `incus start` is also now soft-error aware — it polls `ContainerRunning` before reporting failure, matching the surrounding retry paths, so on nested/CI hosts a container that actually came up (forkstart exits non-zero though it booted) is no longer reported as a failed launch.

- **`[limits.disk] tmpfs_size` now actually resizes `/tmp` (#733)** — `SetTmpfsSize` configured `/tmp` with an `incus config device … disk source=tmpfs` device, but `source=tmpfs` is not a valid Incus disk source, so the device was never created and `/tmp` silently kept its default size (Incus also ignores a `raw.lxc` tmpfs mount for unprivileged containers). It now installs a **systemd `tmp.mount` unit** — a normal in-namespace mount the container's own init performs — sized to the configured value; the size string is parsed to bytes (IEC/SI suffixes or a raw count). Covered by unit tests for the parser and unit builder and an ephemeral shell end-to-end test that checks the in-container `/tmp` size.

- **`coi shell` now honors `[container] storage_pool` (#726)** — only `coi run` and `coi build` read it; `coi shell` (the primary interactive command) dropped it silently and always landed on the Incus default pool, because `session.SetupOptions` had no `StoragePool` field and `session.Setup()`'s `incus init` never passed `-s <pool>`. The pool is now threaded through the shell path (and validated up front like `coi run`), so interactive sessions land on the configured pool. Covered by an ephemeral shell integration test.

- **`coi run` now applies the same container hardening and setup as `coi shell` (#726 follow-up)** — `coi run` and `coi shell` are two separate launch paths, and several settings the shell path applied were silently dropped by `coi run`. Now fixed and each covered by an end-to-end test: **NIC anti-spoofing** (`security.ipv4_filtering`/`mac_filtering`/`port_isolation` on eth0 — without it a restricted/allowlisted `coi run` could spoof its source IP/MAC to bypass its own egress allowlist), the **boot-window egress block** (restricted/allowlist runs now block egress until the real isolation rules land), **pre-boot IPv6 disable** in restricted/allowlist mode, **`[[credentials]]`** seeding, and the **git commit identity + `git.readonly` lock + `useConfigOnly` guard**.

- **Masked the container's `udisks2` service (#706, thanks @blegat)** — a running coi container no longer blocks host suspend / lid-close.

- **`install.sh` initializes a fresh Incus correctly (#703)** — it no longer skips `incus admin init` on real hosts, which left the default profile unusable.

- **Closed more nft rule/set leaks (#696)** — `coi container delete` and the default `coi clean` now fully reclaim a container's firewall rules; monitoring rule installation is idempotent; and `coi health`/orphan cleanup count stale rules accurately.

- **`coi container start` no longer leaves a silently unwritable workspace on OrbStack ≥2.2.2 (#691)** — it applies the UID-mapping fix a pre-upgrade container needs, scoped to stopped coi-managed containers.

- **`coi health` now flags non-thin LVM pools the same way it flags `dir` (#686)**: an `lvm` pool with `lvm.use_thinpool` disabled, or any `lvmcluster` pool (which never gets a thin pool, regardless of config), does a full logical-volume copy on every launch instead of a thin-provisioned CoW clone, the same per-launch cost `dir` has. `#684 follow-up` deferred this because flagging it looked like it needed an extra `incus storage show` call per pool; `incus storage list --format=json` already returns each pool's full config in the same response `evaluatePool`'s driver detection reads, so no extra call is needed. Covered by unit tests for the new detection helper and for the warning on both the usage-ok and usage-error paths.

## 0.11.2 (2026-08-11)

### Fixed

- **`coi health` detects firewalld zone bloat, and the installer prevents it (#695)** — leaked container veths could balloon firewalld's ruleset into the tens of thousands of rules; a new health check flags it, and `install.sh` stops the enrollment on NetworkManager hosts.

- **Closed the first nft teardown leaks from the #696 audit** — `coi kill`/`shutdown` now remove the container's IPv6 egress block, `coi clean --orphans` counts it correctly, and the health hint points at the right command.

## 0.11.1 (2026-08-11)

### Fixed

- **Storage-pool driver check hardened (#684 follow-up)** — the `dir`-driver warning is now sourced from structured data and no longer flakes on realistic CI hosts.

### New Features

- **`[container] session_name` — named sessions that survive workspace moves** — key a session on a name instead of its workspace path, so the same persistent container, slots, and saved history follow you when a checkout moves or is shared across locations. Trusted-scope only.

- **`coi health` flags a `dir` storage pool driver (#659, thanks @technicalpickles)** — a `dir` pool re-unpacks the whole image on every launch; the health check warns and points at recreating it with a CoW driver (zfs/btrfs).

### Fixed

- **Workspace filesystem is checked before using a `shift=true` mount (#683, thanks @technicalpickles)** — coi detects FUSE-backed shares (e.g. OrbStack's) up front and uses `raw.idmap`, fixing silently-unwritable workspaces on OrbStack ≥2.2.2.

- **Reusing a stopped persistent container no longer hard-fails on hosts without idmapped mounts (#685, thanks @technicalpickles)** — reuse applies the same UID-mapping fallback fresh launches use, instead of re-arming a broken config each session.

- **Hosts whose kernel can't do idmapped mounts now fall back automatically (#678, thanks @technicalpickles)** — coi converts to `raw.idmap` and retries instead of failing to launch (e.g. some OrbStack kernels).

- **Fixed a doubled `v` in release versions and a broken `coi update` check (#673, thanks @sklarsa)** — `coi update` no longer wrongly reports you're already on the latest version.

- **Sandbox context no longer grows `~/.claude/CLAUDE.md` every session (#674)** — the injected block is delimited and rewritten in place, and already-bloated files are healed.

## 0.11.0 (2026-07-29)

### Breaking Changes

- **`model` moved to `[tool.claude] model` and is now actually wired** — set the Claude model under `[tool.claude]` (delivered as `ANTHROPIC_MODEL`); a root/`[defaults]` `model` is no longer honored. Migration: move `model = "…"` into a `[tool.claude]` table.

### New Features

- **`COI_TIMING_DEBUG=1` reports where a session's startup time went** — a wall-clock timeline of every `incus`/`nft` call, to help diagnose slow launches.

- **`[[network.hosts]]` and `coi hosts` (#605)** — give a container fixed `/etc/hosts` name→address entries with firewall reachability that matches the active network mode; also manageable at runtime via `coi hosts add/list/remove`. Trusted-scope only.

- **`[defaults] profile` (#607)** — pick the profile a bare `coi` uses when `--profile` isn't passed, while `coi --profile default` still gives a clean container. Trusted-scope only.

- **`coi close` is an alias for `coi shutdown` (#593)** — mirrors the `close` verb you type inside a container.

### Fixed

- **The installer no longer auto-installs ZFS where that can break the system (#666)** — on non-apt distros (e.g. Arch), installing ZFS could break the initramfs; coi now uses ZFS only where it's safe and falls back to btrfs otherwise.

- **`raw.idmap` is set when `code_uid` matches the host UID and shift is off (#667)** — fixes an unwritable `/workspace` on that configuration.

- **Claude Code's dangerous-permissions confirmation is now actually suppressed in sandbox mode (#649)** — non-interactive sandbox startup no longer stalls on the prompt.

- **The installer falls back to btrfs when ZFS can't be set up, and skips ZFS on OrbStack (#661)** — containers no longer silently stay on the slow default storage pool.

- **`[[network.hosts]]` in allowlist mode honors `allow_local_network_access` for private targets (#605)** — a private-address host entry no longer aborts setup when local-network access is enabled.

- **`coi kill` no longer reports a failure when it loses a delete race for a container it killed (#609)** — a container that's already gone now counts as killed.

- **`coi run -- <cmd>` runs with `HOME`/`USER` set (#623)** — `~` and `git config --global` work under `coi run`, matching `coi shell`.

- **A `code_uid` remap no longer aborts setup when a read-only mount lives under `/home/code` (#608, thanks @technicalpickles)**.

- **A persistent container no longer wedges on restart when a protected path was removed from the workspace (#610)** — security mounts are reconciled on restart and re-established for the current workspace.

- **Allowlist mode now works with domains behind rotating IP pools (Vertex, Bedrock, most cloud APIs)** — coi resolves allowlisted domains on the host and writes the same addresses into the container's `/etc/hosts` with DNS egress blocked, so the container and firewall can't disagree and connections stop failing intermittently. Wildcards are rejected up front (they resolved the wrong addresses), and the security monitor no longer false-flags legitimate connections.

- **Allowlist firewall refresh/teardown hardened** — the firewall uses atomic named sets (no fail-closed window during a refresh; rotated-out addresses keep a short grace period), and teardown no longer leaks nft sets across sessions.

- **Typing `close` no longer mislabels a shutting-down container as "kept running" or leaks a stopped ephemeral container (#616)**, plus a round of shutdown-detection hardening (#597).

- **`coi shell --container <missing>` fails fast with a clear error** instead of a misleading 30s timeout.

- **`managed-settings.json` lands root-owned and world-readable (#364 follow-up)** — Claude Code no longer fails OAuth when the host UID differs from the container's code user.

### Security

- **A container in allowlist mode can no longer reach any nameserver** — DNS egress is blocked so it can't learn an address the firewall wasn't already given (the default `8.8.8.8`/`1.1.1.1` entries are gone).

- **`coi kill` no longer fails when the container is already gone, and now reports why a delete actually failed** instead of a bare exit code.

## 0.10.1 (2026-07-12)

### New Features

- **Host port publishing: `[ports] pool` and `[[ports.map]]` (#558)** — publish container services on the host so agent-started servers are reachable at `localhost:<port>`. `pool = N` publishes N identity-mapped ports (same number inside and out, exported as `COI_PORTS`); `[[ports.map]]` entries publish named services on fixed or auto-allocated host ports (exported as `COI_PORT_<NAME>`). Ports are stable per workspace/slot, listen on loopback by default (`listen = "0.0.0.0"` opts into LAN), and ports from an untrusted project config are gated behind `coi trust`.

- **`coi list` shows published ports (#558)** — each container's published ports appear in the text output (`Ports: 23410, web:15432->5432`) and as `published_ports` in `--format json`.

### Fixed

- **`coi list --stopped` no longer titles the output "Active Containers:" (#592)** — the heading now follows the status filter.

- **`close`/poweroff inside `coi shell` is no longer mislabeled as a normal exit (#597)** — an ephemeral container that was shutting down could leak or be reported as "kept running"; cleanup now waits for the real shutdown and honors the contract (ephemeral removed, persistent kept and reported as stopped).

- **`coi tmux capture` / `send` / `list` and `coi attach` no longer target the wrong user's tmux socket (#588)** — these commands defaulted to root's socket and missed background sessions running as the `code` user; they now resolve the code user's actual UID.

## 0.10.0 (2026-07-10)

### Breaking Changes

- **The legacy `claude-on-incus` name is fully retired** — the installer no longer creates the compatibility symlink and removes a leftover one on upgrade (an existing symlink still works).

- **All env-var config overrides removed (`COI_LIMIT_*`, `CLAUDE_ON_INCUS_*`)** — configuration is now config/profiles only (defaults → user config → project config → profile). Set the equivalent `[limits.*]` / `[container]` / `[paths]` keys instead.

- **Config-shaped CLI flags removed** — `--image`, `--persistent`, `--tmux`, `--tool`, `coi build --compression`, and `coi shutdown --timeout` now live in config/profiles (`[container] image`/`persistent`/`shutdown_timeout`, `[shell] use_tmux`, `[tool] name`, `[container.build] compression`). Using a removed flag prints the exact replacement key.

### New Features

- **Generic credential catalog with `[[credentials]]` (#549)** — seed any third-party provider's credential file into the container by referencing a named bundle (`bundle = "ollama"`) or an ad-hoc host/container file pair.

- **`coi list` status filters: `--running`, `--stopped`, `--status <state>` (#578)** — narrow the listing to containers in a given state.

- **Workspace run script: `coi run` with no command runs `./coi-run` in the sandbox** — drop an executable `coi-run` at your workspace root and `coi run` boots the full sandbox and executes it (the shebang decides the interpreter), propagating its exit code.

- **`coi run` streams output live and connects piped stdin** — long builds show output as it's produced, and `cat data | coi run -- ./process.sh` works.

- **`coi run` now starts security monitoring** — arbitrary commands and run scripts get the same watchers as agent sessions.

- **Persistent `coi run` reuses its stopped container** — state actually persists across runs instead of launching a fresh container each time.

- **Resume can change a session's persistence via config** — an explicit `[container] persistent` now wins over the resumed session's recorded mode.

- **Explicit `--profile` wins over the workspace overlay** — a project `.coi/config.toml` can no longer override the profile's `[container]` settings.

- **Built-in `hardened` profile for untrusted repos (#496)** — `--profile hardened` bundles COI's strongest controls (restricted network, secret masking, immutability, ephemeral, no SSH-agent forwarding, monitoring) into one preset.

- **Non-sudoers mode `[network] use_sudo = false` (#508)** — run without the passwordless-sudo nft rule; restricted/allowlist modes fail closed with a clear error, open mode works.

### Improvements

- **Configurable container readiness window** — new `[container] ready_timeout` (default 30s) for slow hosts, and `coi run` now gets the same readiness window as `coi shell`.

- **`coi health` now proves isolation at runtime** — adversarial checks confirm secret masking, host-credential isolation, and metadata/RFC1918 network blocking actually hold, not just the config posture.

- **Broadened the `hardened` profile's default secret-mask set (#496)** — also masks `*.p12`, `*.tfvars`, `*.tfstate`, `.git-credentials`, `kubeconfig`, and more.

### Security

- **Workspace secret-path masking `[security] secret_paths` (#494)** — an opt-in list of workspace globs (`.env`, `*.pem`, `secrets/**`) masked read-only inside the container, so the agent can neither read nor modify them. Fail-closed and symlink-safe.

- **`.claude/settings.json` / `settings.local.json` are now mounted read-only** — a contained agent can't plant a `hooks` command that a later session or a host `claude` run would auto-execute.

- **New `[security] writable_paths` opt-out** — remove specific entries from `protected_paths`; trusted-scope only.

- **All protection-weakening config fields are now trusted-scope only** — an untrusted project config can't disable read-only protections (`disable_protection`, `protected_paths` replace, `host_immutable=false`, `git.writable_hooks`); it can only add protections.

- **`coi run` now protects per-worktree git config (#542)** — `.git/worktrees/<name>/config.worktree` is covered on the `coi run` path too, matching `coi shell`.

### Added

- **The base image can install only the AI agents you use (#454)** — `[container.build] agents = [...]` installs just the listed agents; omitting it installs all of them, as before.

- **Git worktrees / bare-repo checkouts now work inside the container, securely (#533)** — COI mounts the worktree's external gitdir so git commands work, while keeping the hook/config RCE-sink files read-only.

### Fixed

- **`coi file pull` no longer recursively deletes an existing destination directory** — pulling into `.`/`~/` etc. could wipe whole host trees; it now places the pulled entry inside an existing directory, like `cp`/`scp`.

- **Container boot no longer hangs when IPv6 is disabled (#548)** — restricted/allowlist mode no longer wedges `network-online.target` (which stalled `docker.service` and any `After=network-online.target` service). Note: a pre-existing persistent container must be recreated to pick this up.

- **Profile schema accepts every field the code supports** — `stale_base_check` and the two monitoring thresholds now validate.

- **Profile inheritance no longer drops the parent's `sockets` and `env_commands`**.

- **Container git identity is set from your host git config, configurable via `[git]` (#556)** — every tool gets the same commit author; `[git] name`/`email` pin an explicit identity (trusted-scope only).

- **OrbStack is no longer misdetected as Colima/Lima (#553, thanks @technicalpickles)** — fixes an unwritable `/workspace` (files as `nobody:nogroup`) on OrbStack, including in `coi health` (#555).

- **Virtiofs-backed workspaces no longer break `coi run` (#534)** — disk devices attach before start so the isolation fallback covers them (macOS Colima/Lima/OrbStack shared folders).

- **Workspace writes work under Colima/Lima and any host-UID ≠ 1000 (#530)** — `raw.idmap` is set on a UID mismatch so the code user can write `/workspace`.

- **Log-rotation threat detection no longer permanently disables itself after a transient read blip**.

- **Auto-killed containers no longer leak their per-IP firewall rules**.

- **Background monitoring/network diagnostics no longer leak onto the attached terminal (#372)** — refresh, resolver, nft-debug, and auto-stop output go to the session log instead of corrupting the tool's TUI.

- **Monitoring no longer crashes at session start when the GTFOBins detection DB is present (#505)**.

- **opencode is installed for the host CPU architecture instead of always x86_64 (#506)** — fixes arm64 hosts.

## 0.9.0 (2026-06-17)

### Security

- **Out-of-workspace mounts from an untrusted project config now require `coi trust`** — a cloned repo's `.coi/config.toml` could bind-mount the host home directory writable (→ host RCE); such mounts are now dropped unless approved with `coi trust` (`coi trust --list`, `coi untrust` to manage; `COI_TRUST_ALL=1` for CI). Covers project profiles too.

- **`coi file pull` / session-state save no longer recreate container symlinks or special files on the host** — closes a symlink-extraction host-tampering vector.

- **Stale per-IP firewall rules are purged before applying policy** — closes a DHCP-lease-reuse egress bypass where a prior container's leftover ACCEPT could grant a restricted successor open egress.

- **`coi clean --orphans` now also removes orphaned IPv6 drop rules**.

- **Allowlist mode scopes egress to TCP/UDP + rate-limited ICMP** — closes ICMP-tunnel and raw-IP covert channels to allowed hosts.

- **The remaining git config/attribute sinks are now read-only** — `.git/info/attributes`, `.git/config.worktree`, and per-worktree config could carry `filter`/`diff` driver commands that run on the host; a reproduced sandbox escape.

- **Workspace `.coi/` is now read-only inside the container** — an agent can no longer plant project config or profiles applied on the next launch.

- **An untrusted project config can no longer weaken network isolation** — settings that would expose cloud metadata (SSRF) or private networks are dropped with a warning; only strengthening values are honored.

- **The default `coi` image builds from the embedded build script, not the workspace copy** — an agent can't poison `coi-default` by editing `build.sh` in the workspace.

- **Bridge NIC anti-spoofing and port isolation** — prevents an in-container root from spoofing its source IP/MAC to bypass the egress allowlist, and blocks container-to-container lateral movement.

- **IPv6 egress is now enforced host-side** — replaces a container-reversible in-container sysctl; fails closed in restricted/allowlist mode.

- **Boot-time network block now fails closed in restricted/allowlist mode**, and covers persistent-container restarts so planted startup scripts can't get an unrestricted boot window.

### New Features

- **Mint short-lived secrets at session start with `[defaults.env_commands]`** — maps an env var to a host command whose stdout is injected at launch (e.g. a freshly-minted AWS Bedrock/Vault token), so rotating credentials never sit in static config. Trusted-scope only.

- **Forward arbitrary host Unix sockets into the container with `[[sockets]]`** — generalizes SSH agent forwarding to any host socket, enabling credential-broker patterns where the secret never enters the container. Untrusted sockets require `coi trust`.

### Bug Fixes

- **`coi update` on a dev build no longer fails or hides its `--force` guidance when the GitHub API is unavailable** — it refuses offline-safely before any network call.

- **`DirExists`/`FileExists`/`Chown` now handle container paths with spaces or shell metacharacters** — args are passed verbatim instead of through the shell.

- **Bridge firewall rules are no longer removed when `incus list` output can't be parsed** — the default is now to keep rules, so a malformed response can't break other running containers.

- **`coi run` network setup now respects Ctrl+C** — SIGINT is honored during network setup.

- **`max_duration` remaining-time now reports the actual time left** instead of always the full duration.

- **Suspicious-exec pattern matching is now case-insensitive**.

- **Allowlist firewall rules no longer momentarily drop to zero during a DNS refresh** — the rule set is never empty mid-transition, closing a brief unrestricted window.

- **Session metadata no longer corrupts on paths or profile names with special characters** — fixes broken `coi list` / `--resume` for such workspaces.

### Security

- **Sigma `linux/process_creation` rules as a second detection source** — `coi update sigma` sparse-clones community Sigma rules the exec monitor loads alongside GTFOBins patterns.

- **Runtime-loadable exec pattern database** — the exec watcher loads GTFOBins patterns from `~/.coi/gtfobins/` (`coi update patterns [--source <url>]`), on top of an extended compiled-in reverse-shell set.

- **Unified `coi update`** — updates the binary and the pattern database together; `coi update core` / `coi update patterns` for granular control.

- **Sensitive-file access monitored host-side via fanotify** — credential reads and persistence writes (shadow, sudoers, SSH keys, …) raise HIGH/CRITICAL threats, tamper-resistant from inside the container.

- **Network, UDP, and process monitoring read host-side from the container's namespace/cgroup** (`/proc/<init-pid>/net/*`, `/proc` cgroup walk) — an attacker inside the container can no longer hide connections or processes (#430, #432, #428).

- **Fork-bomb and process-spawn-rate detection** — raises CRITICAL (auto-kill capable) when the process count or per-interval spawn rate exceeds a configurable threshold.

- **Host-side auth.log / syslog monitoring** — detects failed logins, invalid users, and sudo/su privilege-escalation attempts (WARNING/HIGH threats).

- **Real-time process-exec monitoring via PROC_EVENTS** — flags reverse shells, netcat `-e`, socket one-liners, and root privilege escalation at exec time.

- **UID-namespace isolation per container** (`security.idmap.isolated`) — eliminates cross-container UID overlap on shared hosts.

- **Docker bridge CIDR isolation** — moves the docker0 bridge and network pool to `172.30/172.31` to avoid conflicts with corporate VPNs/cloud subnets.

### Improvements

- **`coi profile create default` scaffolds a documented starter config** — writes a fully-commented `config.toml` (global, or `./.coi/` with `--project`) with every value commented out, so it overrides nothing until you uncomment a line. Never overwrites an existing config.

- **Stale base image detection** — `coi shell`/`coi run` warn (or `error`/`off` via `stale_base_check`) when a custom image predates its rebuilt base. Resolves #456.

- **`coi build --all`** — builds every profile with a `[container.build]` section, base image first, collecting per-profile errors. Resolves #455.

- **`coi version --format json`** — machine-readable version output.

- **`use_tmux` config option** — set `use_tmux = false` in `[shell]` instead of passing `--tmux=false` every time. Resolves #399.

- **Base image downloaded directly from Canonical's CDN (#388)** — `coi build` fetches Ubuntu from `cloud-images.ubuntu.com` (default `ubuntu:24.04`), avoiding the community image server blocked on some corporate networks; override via `[container.build] base`.

- **Interactive build prompt when the image is missing** — `coi shell`/`coi run` offer to build inline instead of failing; non-interactive use is unchanged.

- **Sudo ownership guidance in SANDBOX_CONTEXT.md (#368)** — tells the tool to `chown` workspace files after `sudo`, which otherwise leaves them root-owned.

**Note:** `coi health --format json` renamed several keys in this release (`firewall`→`nft`, `orphaned_firewall_rules`→`orphaned_nft_rules`, `bridge_firewalld_zone`→`bridge_forward_rules`) as part of the firewalld→nftables naming cleanup — update any consumers.

### Bug Fixes

- **Disk I/O limits now work** — applied as device-level keys on the root disk (the correct Incus API) instead of container-level config.

- **Negative and zero `max_duration` values are now rejected**.

- **`coi run` now enforces `max_duration` at runtime** — previously only `coi shell` honored it.

- **`coi shell --resume` now works when the container is already Running (#413)** — fixes a post-reboot "slot already in use" failure.

- **Error messages no longer suggest removed CLI flags** — they now point at the `config.toml` settings that replaced them (#398).

- **Allowlist IP-refresh logs no longer pollute the terminal** — background refresh output goes to a log file (#372).

- **`poweroff` / `close` now work cleanly in Ubuntu 24.04 containers** — bypasses a systemd-logind transaction conflict; also fixes the `unable to resolve host` warning before sudo.

- **Clearer error when the incus-admin group isn't active yet** — tells you to log out/in or run `newgrp incus-admin` (#383).

- **Escape key now works in nested tmux sessions** — ships `escape-time 10` so Esc reaches opencode/vim promptly (#378).

- **Effort level no longer locked when not configured** — COI only injects `CLAUDE_CODE_EFFORT_LEVEL` when `effort_level` is set, so you can change it mid-session (#376).

- **`coi run` now applies network isolation and SSH agent forwarding from config** — previously it ignored `[network]`/`[ssh]` (#373).

- **Fixed a double `v` prefix (`vv0.8.x`) in the version display**.

- **Session data no longer lost on `sudo poweroff`** — the session-state save retries once the container has stopped instead of failing on a transient SFTP error (#397).

- **Incus errors now surface stderr** — e.g. `Error: Instance not found` instead of a bare `exit status 1` (#276).

### New Features

- **`coi audit` — live threat-event streaming (#362, contributed by @ChrisJr404)** — streams container file/network/exec events as JSON Lines (`--follow`) for piping into a SIEM or `jq`, with no eBPF or daemon install; `--file` re-streams a saved recording.

## 0.8.1 (2026-05-07)

### Improvements

- **Stronger git identity discovery instructions in SANDBOX_CONTEXT.md** — The git auth hints injected into the container context file are now much more directive: they mandate identity discovery before the first commit with a clear priority-ordered sequence (SSH agent → `gh api user` → git log → ask user), explicitly forbid fabricated identities like "code@example.com", and mention that `user.useConfigOnly=true` will block commits without a real identity. This addresses AI tools taking the path of least resistance and committing as "code" despite having SSH agent and/or GitHub token available.

### Bug Fixes

- **Suppress Claude Code auto mode and bypassPermissions prompts in sandbox** — COI now injects `permissions.skipDangerousModePermissionPrompt: true` into settings and writes `/etc/claude-code/managed-settings.json` with `disableAutoMode: "disable"` inside the container. This prevents newer Claude Code versions from showing interactive "Enable auto mode?" or "Everything" confirmation prompts that block sandbox startup. (#364)
- **Fix sandbox settings injection using pure Go instead of Python** — Sandbox settings (e.g., opencode permission bypass, Claude effort level) are now merged into tool config files using Go's `encoding/json` instead of a Python one-liner executed inside the container. This fixes intermittent "exit status 1" failures and eliminates the dependency on `python3` being available inside the container. If the existing config file contains non-standard JSON (comments, trailing commas, BOM), a warning is logged and the file is overwritten with sandbox settings only. (#351, #355)
- **Remove `sg` dependency** — COI no longer uses `sg` (setgid) to wrap incus commands. All platforms now run `incus` directly, which fixes breakage on Linux distros where `sg` is restricted to root (e.g., ALT Linux). Users must ensure their session has `incus-admin` group membership active (log out and back in after `usermod -aG`). (#349)
- **Secure env-var forwarding in tmux sessions** — Forwarded environment variables (e.g. `GITHUB_TOKEN`) are now passed via `tmux new-session -e KEY=VAL` and `tmux set-environment` instead of being inlined as `export KEY=VAL; ...` in the command string. This fixes two issues: secrets no longer appear in `ps auxww` output, and variables now propagate to new tmux windows/panes. (contributed by @SimonArnu, #352)

### New Features

- **Profile auto-resume** — `coi shell --resume` now automatically restores the profile used when the session was originally created. No need to pass `--profile` again. Explicitly passing `--profile` on resume overrides the saved profile. (#342)
- Added `close` command inside containers as an alias for `poweroff`. This provides a safe alternative that doesn't exist on the host machine, preventing accidental host shutdowns when typed outside the container.
- **Better git auth hints in SANDBOX_CONTEXT.md** — When SSH agent and/or GH_TOKEN is forwarded, the context file now includes a `Git Configuration` section that guides AI tools to: prefer SSH over token-based auth for git operations, derive commit identity from the SSH key instead of using "code" as author, and warns that forwarded tokens may have limited scope/permissions. (#337)
- **Git identity guard** — Containers now set `git config --global user.useConfigOnly true` during setup, which forces git to refuse commits until `user.name` and `user.email` are explicitly configured. This prevents AI tools from accidentally committing as the container's default "code" user.

## 0.8.0 (2026-04-16)

### Breaking Changes

- [Breaking] **Host-side immutable protection for protected paths** — COI now `chattr +i`'s protected paths on the host before start, closing the `unshare -m` + `umount` bypass of read-only bind mounts. Needs `CAP_LINUX_IMMUTABLE` (the installer grants it); degrades gracefully on macOS/Colima/Lima; opt out with `host_immutable = false`.

- [Breaking] **Default image renamed `coi` → `coi-default`** — run `coi build` after updating.

- [Breaking] **Removed `coi build custom`** — build custom images through profiles instead.

- [Breaking] **Auto-build on missing image removed** — `coi shell`/`coi run` now error and tell you to `coi build` first.

- [Breaking] **Many CLI flags removed in favor of config/profiles** — `--mount`, `--env`, `--forward-env`, `--network`, `--ssh-agent`, `--timezone`, `--monitor`, all `--limit-*`, and others now live in `config.toml` (see the [Configuration wiki](https://github.com/mensfeld/code-on-incus/wiki/Configuration)).

- [Breaking] **Project config moved from `.coi.toml` to `.coi/config.toml`** (#251), and the `/etc/coi/` and `~/.config/coi/` locations were dropped (only `~/.coi/` and `./.coi/` are scanned now).

- [Breaking] **New `[container]` config section** consolidates image, persistence, storage pool, and build settings; the old `[defaults] image/persistent` and top-level `[build]` keys are rejected with a migration error (#302).

- [Breaking] **Health check `incus_storage_pool` → `incus_storage_pools`** — now a per-pool map in JSON output.

- [Breaking] **`coi resume` renamed to `coi unfreeze`** — avoids confusion with `coi shell --resume`.

### Features

- [Feature] **Container aliases** — `[container] alias = "myproject"` lets you `coi shell/attach/kill/unfreeze myproject` from any directory; slot suffixes like `myproject-2` work (#304).

- [Feature] **Per-profile storage pool** — `[container] storage_pool` routes a project to a specific Incus pool (e.g. fast NVMe vs bulk), validated up front (#302).

- [Feature] **Storage-pool visibility** — `coi list` shows POOL, `coi health` reports per-pool usage, and `coi clean --pools` offers to remove COI containers in unreferenced pools (never deleting the pool itself).

- [Feature] **Guest API disabled by default** (`security.guestapi=false`) — stops containers querying host source paths (which leaked the host username and workspace layout).

- [Feature] **Profiles** — self-contained profile directories under `~/.coi/profiles/` and `./.coi/profiles/`, an embedded built-in `default` profile as the single source of truth for defaults, and profile inheritance via `inherits` (deep-merge, cycle detection, up to 10 levels). Part of #114.

- [Feature] **Read-only mount support** — `readonly = true` on a mount entry safely shares host dirs (e.g. `~/.claude/skills`) without letting the container modify them (#260).

- [Feature] **Self-update command (`coi update`)** — downloads the latest release, verifies its SHA256, and atomically replaces the binary; `--check`/`--force`, sudo auto-escalation, symlink-aware.

- [Feature] **Build configuration in project config** — `[container.build]` defines how to build a custom image (`script` or inline `commands`, `base`) so `coi build` builds it automatically (#251).

- [Feature] **Host timezone inheritance** — containers inherit the host timezone by default; configurable via `[timezone]` (`host`/`fixed`/`utc`) (#236).

- [Feature] **Auto-inject sandbox context into AI tool sessions** — `~/SANDBOX_CONTEXT.md` is loaded into each tool's native context (Claude's `~/.claude/CLAUDE.md`, opencode's `instructions`), preserving any existing user instructions. Opt out with `[tool] auto_context = false` (#243).

- [Feature] **Expanded container toolset with mise-managed runtimes** — adds `fd`, `bat`, `tree`, `strace`, `lsof`, `sqlite3`, Postgres/Redis clients, imagemagick, and mise-managed Python/pnpm/TypeScript/tsx with per-project version pinning.

- [Feature] **SSH agent forwarding** — `[ssh] forward_agent = true` bridges the host `SSH_AUTH_SOCK` into the container so git-over-SSH works without copying keys.

- [Feature] **Environment variable forwarding** — `forward_env = [...]` reads named host vars at session start without storing them in config.

- [Feature] **TTL-aware DNS refresh for allowlist mode** — re-resolves allowed domains on their actual DNS TTL (60s floor) so rotating CDN/cloud IPs stay reachable.

- [Feature] **Safety guards and version checks** — a privileged-container hard block, a security-posture (seccomp/AppArmor) health check, kernel `< 5.15` warnings, and minimum-version checks for Incus (≥ 6.1) and nftables (≥ 0.9.0) (#237, #212, #214).

- [Feature] **Image compression flag** — `--compression` on `coi build` / `coi image publish` (thanks @rominf, #233).

- [Feature] **Auto-trust mise config files in the workspace** — no manual `mise trust` needed inside the container (#328).

### Bug Fixes

- [Bug Fix] **Strengthened sandbox context prompt to reduce unnecessary permission requests** — an explicit "Autonomous Operation" section tells the AI it has full autonomy in its sandbox (#308).
- [Bug Fix] **`coi shell` cleanup no longer prints a scary "Failed to save session data" warning when the tool config dir doesn't exist** — a legitimately-missing directory is treated as benign; real pull failures still surface with full stderr.
- [Bug Fix] **`coi shell` now runs custom `[container.build]` images as `code`, not root** — the user is probed at runtime instead of matched by image alias.
- [Bug Fix] **`coi shell` no longer truncates long outputs to 2000 lines** — the default image ships `history-limit 50000` (#312).
- [Bug Fix] **Non-existent protected paths are now materialized before mounting** — closes a host-persistence attack where an agent could create `.vscode/tasks.json` (etc.) on the writable workspace mount.
- [Bug Fix] **CLI no longer dumps usage/help after output on a non-zero exit** (e.g. degraded `coi health`) (#287).
- [Bug Fix] **`coi health` no longer shows negative free space on a fresh Incus pool** — storage unit suffixes (MiB/GiB/…) are normalized (#285).
- [Bug Fix] **Incus bridge outside the firewalld trusted zone is now auto-fixed at runtime** — no more 30s "Waiting for network…" hang with only a copy-paste hint (#220).
- [Bug Fix] **Build-from-source now fails with actionable messages** when the Go toolchain or `libsystemd-dev` is missing (including the `sudo` strips-PATH pitfall).
- [Bug Fix] **Profile operations no longer mutate global config** (pointer aliasing), and profile inheritance now merges `additional_protected_paths` instead of replacing them.
- [Bug Fix] **`attach` now respects the global `--workspace` flag**, and `list`/`info`/`clean`/`persist`/`monitor`/`health` now respect `--profile` (they previously reloaded config and dropped it).
- [Bug Fix] **`coi run` and 62 other call sites now clean up on non-zero exit** — replaced direct `os.Exit()` with cobra error returns so deferred container/firewall cleanup runs.
- [Bug Fix] **IPv6 bypass of all network isolation rules closed** — IPv6 is disabled in the container in restricted/allowlist modes.
- [Bug Fix] **Allowlist refresh no longer leaves an unprotected window** — new rules are applied before old ones are removed.
- [Bug Fix] **`StopGraceful` semantics no longer inverted** — a graceful stop is no longer a force-stop; adds a 5s force-stop escalation.
- [Bug Fix] **Dynamic UID mapping for workspace mounts** — `raw.idmap` maps a mismatched host UID to the code user, fixing "Permission denied" on non-1000 hosts and CI (#226).
- [Bug Fix] **IPv4 preferred for Claude CLI install in containers** — avoids IPv6 timeouts/403s (#224).
- [Bug Fix] **Raw-iptables fallback for Docker's FORWARD DROP without firewalld** — fixes a "Waiting for network…" hang when Docker is installed (#83).
- [Bug Fix] **`install.sh` now works correctly under `curl | bash`** — interactive detection and `read` use `/dev/tty` (#215, #222, thanks @dgrant).

### Enhancements

- [Enhancement] **`coi update` restores `cap_linux_immutable` after replacing the binary** (or prints the manual `setcap` command).
- [Enhancement] **`coi profile create` / `edit` / `delete`** — manage profiles from the CLI instead of hand-editing config (#114).
- [Enhancement] **Standardized CLI table output** across `snapshot list`, `image list`, `profile list`, `tmux list`, with `--format text|json` (#141).
- [Enhancement] **`coi monitor` auto-detects the container** from the workspace, with a `--workspace` override (#112).
- [Enhancement] **Expanded env-scanning detection** — catches Python/Node/Ruby/awk env reads and `/proc/*/environ` access via `strings`/`xxd`/`hexdump`.

### Improvements

- [Improvement] **Installer detects active ufw before installing firewalld** — avoids the silent container-networking breakage when both manage netfilter; adds a `ufw_conflict` health check (#281).
- [Improvement] **`-a`/`--all` and `-f`/`--force` short flags** added across the relevant commands.
- [Improvement] **`profile show` renamed to `profile info`** (old verb kept as a hidden alias); new `container info` / `image info` subcommands; `coi images` removed (use `coi image list`).
- [Improvement] **Installer auto-initializes Incus** (`incus admin init --auto`) on fresh installs and quiets its raw command output.
- [Improvement] **Profiles loaded from both `~/.coi/profiles/` and `./.coi/profiles/`** — a name defined in both is a hard error.

## 0.7.0 (2026-03-10)

### Bug Fixes

- [Fix] **opencode session resume in ephemeral mode** — `--continue` now resumes correctly, with the SQLite session DB persisted on the host mount across container recreation (#196). Also fixed opencode session detection (#183), XDG config location (#158), interactive permission mode (#186), and the install URL (#157).
- [Fix] **`coi build --force` works from any directory** — build assets are embedded via `//go:embed` (#176).
- [Bug Fix] **Docker Compose now works inside session containers** — nesting/sysctl-intercept flags are set before first boot on the `coi shell` path, and `ip_unprivileged_port_start` is pre-set to avoid an AppArmor-blocked write (#187).
- [Bug Fix] **Docker works without `sudo` for the `code` user** — the socket is created with group `code` (#134).
- [Bug Fix] **Persistent-session resume reuses the stopped container** instead of creating a fresh one, so system-level changes aren't lost (#190).
- [Bug Fix] **Container user UID/GID remapped for a non-default `code_uid`** — fixes "Permission denied" / "I have no name!" (#166).
- [Bug Fix] **Config merge no longer silently drops boolean settings** — security-critical defaults (`block_private_networks`, `auto_kill_on_critical`, …) survive multi-layer merges (converted to `*bool`).
- [Bug Fix] **`[incus]` config values are now actually applied** to Incus command execution (`project`, `group`, `code_uid`, `code_user`).
- [Bug Fix] **`settings.json` merge preserves the user's `env` section** (deep merge) — no longer drops e.g. AWS Bedrock vars.
- [Bug Fix] **`preserve_workspace_path` honored everywhere** — `coi attach`/`run`/`container exec` and protected-path mounts use the dynamic workspace path, with guards against mounting over system dirs.
- [Bug Fix] **`/tmp` exhaustion no longer hangs agents silently** — `/tmp` is backed by the root disk by default (opt-in RAM tmpfs via `tmpfs_size`), with auto-cleanup of stale files (#135).
- [Bug Fix] **NFT/firewall rule cleanup completed across all termination paths** — `coi shutdown`, `coi container delete`, `coi kill`, and responder auto-kill all clean firewall + NFT-monitoring rules and veth zone bindings, fixing rule accumulation that could hang the system (#119, #130). RFC1918 host traffic is no longer flagged in open mode.
- [Bug Fix] **NFT monitor / security-monitoring output no longer corrupts the TUI or spams alerts** — errors route through `OnError`, threats dedupe in a 30s window, and only pause/kill actions print to stderr.
- [Bug Fix] **Cross-device / symlink handling when saving session data** — `PullDirectory` falls back to recursive copy on `EXDEV` and recreates symlinks (thanks @psaab, #106).

### Features

- [Feature] **Security Monitoring System** — always-on, host-side monitoring detects reverse shells, data exfiltration, secret scanning, and unexpected network connections, escalating log → alert → pause → kill with a JSONL audit log. `coi monitor` shows a live dashboard. Addresses #112.
- [Feature] **nftables-based network monitoring** — kernel-level, tamper-resistant visibility into all container network activity (including short-lived and blocked connections), flagging metadata-endpoint access, suspicious ports, and allowlist violations.
- [Feature] **Large-write and disk-space detection** — flags large filesystem writes (exfil vector) and `/tmp` above 80%.
- [Feature] **opencode support** — opencode is a supported tool (`[tool] name = "opencode"` / `coi shell --tool opencode`), installed in the base image, with permission-bypass config and `--continue` resume (#117).
- [Feature] **Configurable permission mode** — `[tool] permission_mode` toggles `bypass` (default) vs `interactive` (human-in-the-loop), for Claude and opencode.
- [Feature] **Configurable protected paths** — `[security]` `protected_paths` / `additional_protected_paths` / `disable_protection` control the read-only set (defaults: `.git/hooks`, `.git/config`, `.husky`, `.vscode`); symlinks rejected.
- [Feature] **`preserve_workspace_path`** — mount the workspace at its host absolute path instead of `/workspace`, so path-relative session data persists (#108).
- [Feature] **Claude effort-level config** — `[tool.claude] effort_level` prevents interactive prompts in autonomous sessions.
- [Feature] **`coi unfreeze`** — unfreeze a security-paused container (or all frozen COI containers).
- [Feature] **`--tool` flag for `coi shell`** — override the configured tool for one session.
- [Feature] **New health checks** — Incus storage pool, container connectivity (real in-container DNS/HTTP test), and network restriction (verifies restricted mode actually blocks private IPs) (#102).
- [Feature] **`coi container list` and `-t/--tty` for `coi container exec`** — low-level listing and PTY allocation for programmatic/interactive use (#123, #124).
- [Feature] **Base image adds ripgrep and fzf**.

## 0.6.0 (2026-02-02)

### Bug Fixes

- [Bug Fix] **`settings.json` is now merged, not overwritten** — user config (AWS Bedrock creds, env vars, custom settings) is preserved when sandbox permissions are added (#76).
- [Bug Fix] **`coi list --all` session listing fixed** — the Saved Sessions section always appears, and detection is tool-agnostic via `tool.ConfigDirName()` instead of hardcoded `.claude` (#81).
- [Bug Fix] **Image-build DNS auto-fix broadened** — handles localhost/`127.x.x.x` and missing nameservers, fixing a "Waiting for network…" hang (#83).

### Features

- [Feature] **Resource and time limits** — `[limits]` controls CPU, memory, disk I/O, max processes, and a `max_duration` after which the container is auto-stopped (#71).
- [Feature] **`coi health` command** — verifies dependencies (Incus, permissions, image age, bridge, firewalld, storage, …) with `--format json` and exit codes 0/1/2.
- [Feature] **Firewalld-based network isolation** — replaces OVN/OVS with firewalld FORWARD-chain rules scoped by container IP, working on any standard Incus bridge.
- [Feature] **Automatic Docker/nested-container support** — sets the nesting/syscall-intercept flags so Docker works out of the box.
- [Feature] **Automatic Colima/Lima detection** — disables UID shifting inside those VMs (which handle mapping themselves); manual override via `[incus] disable_shift`.
- [Feature] **AWS Bedrock validation for Colima/Lima** — fails fast with actionable errors when the Bedrock/`.aws` setup is incomplete (#76).
- [Feature] **`coi snapshot`** — create/list/restore/delete container snapshots (optionally stateful) for checkpoint/rollback workflows (#72).
- [Feature] **`coi persist`** — convert running ephemeral containers to persistent (`--all`, `--force`).
- [Feature] **`coi list` shows IPv4 addresses** for running containers (#66).

### Enhancements

- [Enhancement] **Claude CLI now installed via the official native installer** instead of the deprecated npm package; rebuild the base image with `coi build --force` (#82).
- [Enhancement] **macOS/Colima docs and UX** — clearer setup and an open-mode-without-firewalld warning.

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
