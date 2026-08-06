<p align="center">
  <img src="misc/logo.png" alt="Code on Incus Logo" width="350">
</p>

# code-on-incus (`coi`)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mensfeld/code-on-incus)](https://golang.org/)
[![Latest Release](https://img.shields.io/github/v/release/mensfeld/code-on-incus)](https://github.com/mensfeld/code-on-incus/releases)
[![Join the chat at https://slack.karafka.io](https://raw.githubusercontent.com/karafka/misc/master/slack.svg)](https://slack.karafka.io)

**Isolated machines for AI coding agents - with active defense.**

COI gives each AI agent its own machine - a full system container with root access, systemd, Docker, and the ability to install anything. Agents work like they would on a real server: run services, manage packages, use cron - without touching your actual system. Files stay correctly owned, no permission hacks needed.

Your credentials stay on the host. SSH keys, environment variables, and Git tokens are never exposed to AI tools unless you explicitly mount them. If something goes wrong, COI catches it - reverse shells, credential scanning, data exfiltration - and pauses or kills the container automatically. No manual intervention needed.

Built by developers, for developers who run AI agents and want to know what those agents are doing. Not a product, not a startup - a tool that does the job.

## Who this is for

- You run AI coding agents and want them to have full machine access - root, Docker, package managers, services - without risking your host
- You want to know when an agent does something suspicious, not find out after the fact
- You run multiple agents in parallel and need them isolated from each other
- You want persistent dev environments that survive restarts and reboots, not throwaway containers that lose your setup every time
- You care about your credentials not ending up inside an agent-controlled environment

<p align="center">
  <a href="https://www.youtube.com/watch?v=t78-JUnTK5Q">
    <img src="https://img.youtube.com/vi/t78-JUnTK5Q/maxresdefault.jpg" alt="BetterStack video about Code on Incus" width="600">
  </a>
  <br>
  <em>Watch the BetterStack video about Code on Incus</em>
</p>

![Demo](misc/demo.gif)

## Table of Contents

- [Who this is for](#who-this-is-for)
- [Supported AI Coding Tools](#supported-ai-coding-tools)
- [Supported Tools (detailed)](https://github.com/mensfeld/code-on-incus/wiki/Supported-Tools)
- [Features](#features)
- [Quick Start](#quick-start)
- [Why Incus Instead of Docker or Docker Sandboxes?](#why-incus-instead-of-docker-or-docker-sandboxes)
- [Installation](#installation)
- [macOS Support](#macos-support)
- [Usage](#usage)
- [Run Scripts and Commands in the Sandbox](#run-scripts-and-commands-in-the-sandbox)
- [Session Resume](#session-resume)
- [Persistent Mode](#persistent-mode)
- [Configuration](#configuration)
- [Profiles](#profiles)
- [Resource and Time Limits](https://github.com/mensfeld/code-on-incus/wiki/Resource-and-Time-Limits)
- [Container Lifecycle & Session Persistence](https://github.com/mensfeld/code-on-incus/wiki/Container-Lifecycle-and-Sessions)
- [Network Isolation](#network-isolation)
- [Security Monitoring](#security-monitoring)
- [Security Best Practices](https://github.com/mensfeld/code-on-incus/wiki/Security-Best-Practices)
- [Snapshot Management](https://github.com/mensfeld/code-on-incus/wiki/Snapshot-Management)
- [System Health Check](https://github.com/mensfeld/code-on-incus/wiki/System-Health-Check)
- [Troubleshooting](https://github.com/mensfeld/code-on-incus/wiki/Troubleshooting)
- [FAQ](https://github.com/mensfeld/code-on-incus/wiki/FAQ)

## Supported AI Coding Tools

Currently supported:
- **Claude Code** (default) - Anthropic's official CLI tool
- **opencode** - Open-source AI coding agent (https://opencode.ai)
- **pi** - AI coding assistant (https://pi.dev)

Coming soon:
- Aider - AI pair programming in your terminal
- Cursor - AI-first code editor
- And more...

**Tool selection** is config/profile-driven:
```toml
# ~/.coi/config.toml or ./.coi/config.toml
[tool]
name = "opencode"            # or "claude" (default), "pi"
```
```bash
coi shell                    # Uses the configured tool (Claude Code by default)
coi shell --profile opencode # Or switch via a profile with [tool] name = "opencode"
```

**Permission mode** - Control whether AI tools run autonomously or ask before each action:
```toml
# ~/.coi/config.toml or .coi/config.toml
[tool]
name = "claude"              # Default AI tool
permission_mode = "bypass"   # "bypass" (default) or "interactive"
```

See the [Supported Tools wiki page](https://github.com/mensfeld/code-on-incus/wiki/Supported-Tools) for detailed configuration, API key setup, and adding new tools.

## Features

**Core Capabilities**
- Multi-slot support - Run parallel AI coding sessions for the same workspace with full isolation
- Session resume - Resume conversations with full history and credentials restored (workspace-scoped)
- Persistent containers - Keep containers alive between sessions (installed tools preserved)
- Workspace isolation - Each session mounts your project directory
- Slot isolation - Each parallel slot has its own home directory (files don't leak between slots)
- **Workspace files persist even in ephemeral mode** - Only the container is deleted, your work is always saved
- Container snapshots - Create checkpoints, rollback changes, and branch experiments with full state preservation

**Host Integration**
- SSH agent forwarding - Use git-over-SSH inside containers without copying private keys (`[ssh] forward_agent = true`)
- Host port publishing - Publish container TCP ports on the host (`[ports] pool` for identity-mapped agent-usable ports, `[[ports.map]]` for fixed services): agent-started dev servers become reachable at `localhost:<port>`, with per-slot deterministic allocation, a pre-launch conflict check, and `coi trust` gating for untrusted project configs
- Host socket forwarding - Forward arbitrary host Unix sockets into the container (`[[sockets]]`) so the host endpoint never enters the container — the building block for credential brokers (mint short-lived tokens on the host, fetch them on demand inside). Untrusted project-config sockets are gated behind `coi trust`
- Credential catalog - Copy third-party provider credentials into the container via `[[credentials]]` entries (config or profile): reference a named catalog bundle (`bundle = "ollama"`) or declare an ad-hoc host/container file pair for anything not yet cataloged. `claude`/`opencode`/`pi`'s own credential files come from the same built-in catalog. Ad-hoc entries from an untrusted project `.coi/config.toml` are gated behind `coi trust`; catalog references carry the same trust level the built-in tool credentials already have
- Environment variable forwarding - Selectively forward host env vars by name (`forward_env` in config)
- Command-sourced env vars - Mint a fresh secret per session by running a host command at start and injecting its output as an env var (`[defaults.env_commands]`) — for short-lived API keys/tokens. Trusted-scope config only
- Host timezone inheritance - Containers automatically inherit the host's timezone (configurable via `[timezone]` config)
- Sandbox context file - Auto-injected `~/SANDBOX_CONTEXT.md` tells AI tools about their environment (network mode, workspace path, persistence, etc.). Automatically loaded into each tool's native context system: Claude Code via `~/.claude/CLAUDE.md`, OpenCode via the `instructions` field in `opencode.json`, pi via `~/.pi/agent/APPEND_SYSTEM.md` symlink (opt out with `auto_context = false`)

**Security & Isolation**
- Credential protection - SSH keys, `.env` files, Git credentials, and environment variables are **never** exposed unless explicitly mounted
- Privileged container guard - Refuses to start when `security.privileged=true` is detected, which defeats all container isolation
- Security posture verification - `coi health` checks seccomp, AppArmor, and privilege settings to confirm full isolation
- Kernel version enforcement - Warns on host kernels below 5.15 that may lack security features for safe isolation
- Real-time threat detection - Kernel-level nftables monitoring detects reverse shells, C2 connections, data exfiltration, DNS tunneling, and credential scanning
- Automated response - Auto-pause on HIGH threats, auto-kill on CRITICAL — no manual intervention needed
- Network isolation - nftables-based restricted/allowlist/open modes block private network access and prevent exfiltration
- Protected paths - `.git/hooks`, `.git/config`, `.husky`, `.vscode` mounted read-only to prevent supply-chain attacks
- Host-side immutable protection - Protected paths are locked with `chattr +i` during sessions, preventing `unshare -m` + `umount` bypass of read-only mounts (opt out: `[security] host_immutable = false`)
- Git identity guard - Containers enforce `user.useConfigOnly=true`, preventing AI tools from committing as the default "code" user
- Guest API disabled - Incus guest API (`/dev/incus`) disabled by default, preventing host path and topology leaks
- System containers - Full OS isolation with unprivileged containers, better than Docker privileged mode
- Automatic UID mapping - No permission hell, files owned correctly
- Audit logging - All security events logged to JSONL for forensics and compliance

**Safe Dangerous Operations**
- AI coding tools often need broad filesystem access or bypass permission checks
- **These operations are safe inside containers** because the "root" is the container root, not your host system
- Containers are ephemeral - any changes are contained and don't affect your host
- This gives AI tools full capabilities while keeping your system protected

## Quick Start

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/mensfeld/code-on-incus/master/install.sh | bash

# Build image (first time only, ~5-10 minutes)
coi build

# Start coding with your preferred AI tool (defaults to Claude Code)
cd your-project
coi shell

# Or use opencode instead (config-driven: [tool] name = "opencode",
# or a profile: coi shell --profile opencode)

# That's it! Your AI coding assistant is now running in an isolated container with:
# - Your project mounted at /workspace
# - Correct file permissions (no more chown!)
# - Full Docker access inside the container
# - GitHub CLI available for PR/issue management
# - All workspace changes persisted automatically
# - No access to your host SSH keys, env vars, or credentials
```


## Why Incus Instead of Docker or Docker Sandboxes?

Incus is a modern Linux container and virtual machine manager, forked from LXD. Unlike Docker (which uses application containers), Incus provides **system containers** that behave like lightweight VMs with full init systems.

### Security Comparison

| Capability | **code-on-incus** | Docker Sandbox | Bare Metal |
|------------|-------------------|----------------|------------|
| **Credential isolation** | Default (never exposed) | Partial | None |
| **Real-time threat detection** | Kernel-level (nftables) | No | No |
| **Reverse shell detection** | Auto-kill | No | No |
| **Data exfiltration alerts** | Auto-pause | No | No |
| **Network isolation** | nftables (3 modes) | Basic | No |
| **Protected paths** | Read-only mounts | No | No |
| **Auto response (pause/kill)** | Yes | No | No |
| **Audit logging** | JSONL forensics | No | No |
| **Supply-chain attack prevention** | Git hooks/IDE configs protected | No | No |

### Why Incus Instead of Docker Sandboxes?

- **Linux-first, not Linux-last.** Docker Sandboxes' microVM isolation is only available on macOS and Windows. Linux gets a legacy container-based fallback. COI is built for Linux from the ground up because Incus is Linux-native.

- **No Docker Desktop required.** Docker Sandboxes is a Docker Desktop feature. Docker Desktop is not open source and has commercial licensing requirements for larger organizations. COI depends only on Incus - fully open source, no vendor lock-in, no additional runtime.

- **System containers, not containers-in-VMs.** Incus system containers run a full OS with systemd and native Docker support inside - one clean isolation layer. Docker Sandboxes nests application containers inside microVMs, adding architectural complexity.

- **No permission hell.** Incus automatic UID/GID shifting means files created by agents have correct ownership on the host. No mapping hacks needed. (Note: files created via `sudo` in the workspace will be root-owned — the sandbox context file instructs AI tools to fix ownership after sudo operations.)

- **Credential isolation by default.** Host environment variables, SSH keys, and Git credentials are never exposed to AI tools unless explicitly mounted.

- **Simple and transparent.** No separate daemon, no opaque VM nesting. COI talks directly to Incus - easy to inspect, debug, and extend.

## Installation

### Automated Installation (Recommended)

```bash
# One-shot install
curl -fsSL https://raw.githubusercontent.com/mensfeld/code-on-incus/master/install.sh | bash

# This will:
# - Download and install coi to /usr/local/bin
# - Check for Incus installation
# - Verify you're in incus-admin group
# - Show next steps
```

**Manual installation:** Download the binary from [GitHub Releases](https://github.com/mensfeld/code-on-incus/releases), make it executable, and move to `/usr/local/bin/`. Requires Linux with Incus installed and user in the `incus-admin` group. **You must log out and back in** (or run `newgrp incus-admin`) after adding your user to the group — COI runs `incus` directly and requires the group to be active in your session. See the [Incus installation guide](https://linuxcontainers.org/incus/docs/main/installing/) for setting up Incus.

### Build Images

```bash
# Build the default coi-default image (5-10 minutes)
coi build

# Build without compression (faster iteration):
# set [container.build] compression = "none" in config or the profile
coi build

# Build a custom image via a profile
coi profile create my-image
# Edit .coi/profiles/my-image/config.toml: set [container] image and a [container.build] section
coi build --profile my-image

# Build images for all profiles that have a [container.build] section
coi build --all

# Rebuild all profile images from scratch
coi build --all --force
```

**What's included in the `coi-default` image:**
- Ubuntu 24.04 base with Docker (full Docker-in-container support)
- **mise** (polyglot runtime manager) — Python 3, pnpm, TypeScript, tsx pre-installed; add more with `mise use go@latest`, `mise use ruby@3`, etc.
- Node.js 22 LTS (system, for Claude CLI) + npm
- Claude Code CLI (default AI tool) + GitHub CLI (`gh`)
- tmux, git, curl, build-essential, and common build tools
- Modern CLI utilities: fd-find, bat, tree
- Debugging tools: strace, lsof
- Database clients: sqlite3, postgresql-client, redis-tools
- imagemagick for image processing

**Custom images:** Build your own specialized images using profile-based build scripts that run on top of the base `coi-default` image. See the [Image Management wiki page](https://github.com/mensfeld/code-on-incus/wiki/Image-Management) for complete profile-based build workflows.

## macOS Support

**COI works on macOS** using [Colima](https://github.com/abiosoft/colima) or [Lima](https://github.com/lima-vm/lima) VMs. See the [macOS Setup Guide](https://github.com/mensfeld/code-on-incus/wiki/macOS-Setup-Guide) for complete instructions.

## Usage

### Basic Commands

```bash
# Interactive session (defaults to Claude Code)
coi shell

# Use a different AI tool (config/profile-driven: [tool] name = "opencode")
coi shell --profile opencode

# Use specific slot for parallel sessions
coi shell --slot 2

# Resume previous session
coi shell --resume

# Run a command in the sandbox (streams output, propagates exit code)
coi run -- npm test

# Run the workspace run script (./coi-run) in the sandbox
coi run

# Attach to existing session
coi attach

# Real-time security monitoring dashboard
coi monitor

# View session logs (setup messages, network notices, errors)
coi logs                        # Auto-detect container from current workspace
coi logs coi-abc123-1 -f        # Tail logs live

# Stream the structured (JSON Lines) threat-event audit log for a session
coi audit coi-abc123-1 -f

# Approve out-of-workspace mounts / forwarded sockets from a project .coi/config.toml
coi trust                       # approve   (coi trust --list to view, coi untrust to revoke)

# List active containers and saved sessions
coi list --all
coi list --running              # Only running containers (also: --stopped, --status frozen)

# Gracefully shutdown / force kill containers
coi shutdown coi-abc12345-1
coi kill --all

# Cleanup stopped containers and orphaned resources
coi clean
coi clean --pools             # Detect containers in unused storage pools

# Update coi to the latest release
coi update
```

> **Upgrading to 0.10?** 0.10 removes all config-shaped CLI flags (`--image`, `--persistent`, `--tmux`, `--tool`, `coi build --compression`, `coi shutdown --timeout`) and the legacy `CLAUDE_ON_INCUS_*` / `COI_LIMIT_*` env-var overrides — everything config-shaped now lives in config files and profiles, and a removed flag fails with a hint naming its replacement key. See the [Upgrading from 0.9 to 0.10 guide](https://github.com/mensfeld/code-on-incus/wiki/Migration-Guide#upgrading-from-09-to-010) (the [0.8→0.9 notes](https://github.com/mensfeld/code-on-incus/wiki/Migration-Guide#upgrading-from-08-to-09) are there too).

### Container Aliases

Assign human-friendly names to containers for easy management from any directory:

```toml
# .coi/config.toml (in your project)
[container]
alias = "myproject"
```

```bash
coi shell myproject              # Launch session using alias (from any directory)
coi attach myproject             # Attach to running aliased container
```

See the [Container Lifecycle and Sessions guide](https://github.com/mensfeld/code-on-incus/wiki/Container-Lifecycle-and-Sessions#container-aliases) for full alias documentation.

### Global Flags

```bash
--workspace PATH        # Workspace directory to mount (default: current directory)
--slot NUMBER           # Slot number for parallel sessions (0 = auto-allocate)
--resume [SESSION_ID]   # Resume from session (omit ID to auto-detect latest for workspace)
--continue [SESSION_ID] # Alias for --resume
--profile NAME          # Use named profile
```

Everything else — image selection, persistence, network mode, mounts, socket forwarding, environment variables, SSH agent, monitoring, timezone, resource limits — is configured via config files or profiles, not flags (the former `--image` and `--persistent` flags were removed in 0.10; set `[container] image` / `persistent` instead). See the [Configuration wiki page](https://github.com/mensfeld/code-on-incus/wiki/Configuration) for the full reference.

### Advanced Usage

See the wiki for detailed documentation:

- **[Container Operations](https://github.com/mensfeld/code-on-incus/wiki/Container-Operations)** - Container management and low-level operations
- **[File Transfer](https://github.com/mensfeld/code-on-incus/wiki/File-Transfer)** - Push/pull files between host and containers
- **[Tmux Automation](https://github.com/mensfeld/code-on-incus/wiki/Tmux-Automation)** - Automate AI sessions with tmux commands
- **[Image Management](https://github.com/mensfeld/code-on-incus/wiki/Image-Management)** - Create and manage custom images
- **[Snapshot Management](https://github.com/mensfeld/code-on-incus/wiki/Snapshot-Management)** - Create checkpoints and rollback changes

## Run Scripts and Commands in the Sandbox

COI's isolation isn't only for AI agents — `coi run` executes regular commands
and scripts with the same protection: workspace mount, read-only protected
paths, secret masking, network isolation, resource/time limits, and security
monitoring. Output streams live, stdin is connected, and the command's exit
code becomes `coi run`'s exit code.

```bash
# Arbitrary commands
coi run -- npm test
coi run -- make build
cat data.csv | coi run -- ./process.sh

# Workspace run script: with no command, coi runs ./coi-run
cat > coi-run <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
npm ci && npm test
EOF
chmod +x coi-run
coi run
```

The run script is executed **directly from the workspace mount** — it comes
from the host; nothing is copied into the container. It is extensionless and
must be executable: the shebang decides the interpreter, so a bash, ruby, or
python `coi-run` all work the same way. The container is cleaned up when the
script finishes — or kept, with `[container] persistent = true`, so installed
packages and caches survive between runs.

**Security note:** a cloned repository can ship its own `coi-run`, so
`coi run` in a repo you don't trust executes that repo's code — inside the
sandbox, which is exactly what the sandbox is for. For untrusted projects, use
a credential-limiting profile (e.g. `coi run --profile hardened`, or your own
profile with `[ssh] forward_agent = false` and a restricted network mode) so
the script gets no SSH agent, forwarded env, or open egress.

## Session Resume

Resume a previous AI coding session with full history and credentials restored:

```bash
coi shell --resume              # Auto-detect latest session for this workspace
coi shell --resume=<session-id> # Resume specific session
coi list --all                  # List available sessions
```

**What's restored:** Full conversation history, tool credentials, user settings, and project context. The profile used when the session was created is also automatically restored — no need to pass `--profile` again (explicitly passing `--profile` overrides the saved one). Sessions are workspace-scoped — `--resume` only finds sessions from the current workspace directory.

See the [Container Lifecycle and Sessions guide](https://github.com/mensfeld/code-on-incus/wiki/Container-Lifecycle-and-Sessions) for details on how session persistence works.

## Persistent Mode

By default, containers are **ephemeral** (deleted on exit). Your **workspace files always persist** regardless of mode.

Enable **persistent mode** to also keep the container and its installed packages:

```toml
# ~/.coi/config.toml, ./.coi/config.toml, or a profile
[container]
persistent = true
```

**What persists:**
- **Ephemeral mode:** Workspace files + session data (container deleted)
- **Persistent mode:** Workspace files + session data + container state + installed packages, system setup

See the [Container Lifecycle and Sessions guide](https://github.com/mensfeld/code-on-incus/wiki/Container-Lifecycle-and-Sessions) for details.

## Configuration

Config file: `~/.coi/config.toml`

```toml
[container]
image = "coi-default"
persistent = true
# storage_pool = ""            # Empty = Incus default pool
# alias = "myproject"          # Human-friendly name for this workspace's containers

[tool]
name = "claude"
permission_mode = "bypass"
# auto_context = true          # Auto-inject sandbox context into tool's native system
```

**Configuration hierarchy** (highest precedence last):
1. Built-in defaults
2. User config (`~/.coi/config.toml`, or the file `$COI_CONFIG` points at)
3. Project config (`./.coi/config.toml`)
4. Profile (`--profile <name>`)

Config-shaped settings have no CLI flags and no env-var overrides — config
and profiles are the single source of truth. The remaining CLI flags are
per-invocation choices only: `--workspace`, `--slot`, `--resume`, `--profile`.

Place a `.coi/config.toml` in any repository root to auto-configure COI for that project — useful for teams to share container image, environment, and resource limits.

See the [Configuration wiki page](https://github.com/mensfeld/code-on-incus/wiki/Configuration) for the full config reference, per-repo setup, profiles, and environment variables.

### Forwarding host sockets, minting secrets & copying credential files

Three ways to give containerized tools credentials:

- **`[[sockets]]`** forwards any host Unix socket into the container via an Incus proxy device, so the host endpoint never crosses in — the building block for **credential brokers** (a host process mints short-lived tokens; an in-container `credential_process` fetches them on demand).
- **`[defaults.env_commands]`** runs a host command at session start and injects its trimmed stdout as an env var — for plain env-var credentials (e.g. an AWS Bedrock bearer token). Trade-off: the value lives in the container env for the session, so prefer the broker for high-value/rotatable secrets.
- **`[ports]`** publishes container TCP ports on the host, so services the agent starts are reachable at `localhost:<port>`: `pool = 3` gives every session identity-mapped ports (the agent binds a pool number, you open the SAME number — the sandbox context file tells the agent to use them), and `[[ports.map]]` publishes fixed container ports (`name = "web"`, `container = 3000`) auto-allocated or pinned on the host side. Deterministic per workspace/slot, preflight-checked before launch, isolation-neutral (userspace proxy, no NAT rules); `coi list` shows each container's published ports. See the [Port Publishing wiki page](https://github.com/mensfeld/code-on-incus/wiki/Port-Publishing).
- **`[[credentials]]`** copies static credential files from host to container at session setup — for tools that read credentials from disk rather than an env var. Use `bundle = "ollama"` to reference a name from COI's built-in catalog (the same catalog `claude`/`opencode`/`pi` use for their own credentials), or set `host`/`container` (plus optional `mode`) for an ad-hoc file not yet in the catalog. Missing host files are skipped with a log line rather than failing the session.

Sockets, `[ports]`, and ad-hoc `[[credentials]]` entries are gated behind `coi trust` when they come from an untrusted project `.coi/config.toml`; `env_commands` from one is ignored outright; catalog-referenced credentials are never gated (the host path is fixed by COI's own catalog, not the referencing config). See the [Configuration wiki page](https://github.com/mensfeld/code-on-incus/wiki/Configuration) for full examples and the trust model.

## Profiles

Profiles are reusable container configurations bundling image, tool, limits, mounts, sockets, build scripts, context files, and environment into named templates.

```bash
coi shell --profile rust-dev                 # Use a profile
coi profile create rust-dev                  # Create a new profile (then edit its config.toml)
coi profile list                             # List all profiles
```

Each profile is a self-contained directory (`.coi/profiles/<name>/`) bundling a `config.toml` plus optional build script and context file. Profiles support inheritance (`inherits = "parent"`), context files for AI-agent instructions, and custom build scripts. COI also ships a JSON Schema for profile configs (`coi schema profile`) so external tools can validate them. See the [Profiles wiki page](https://github.com/mensfeld/code-on-incus/wiki/Profiles) for the full reference, examples, and schema details.

### Opening an untrusted repo safely

For inspecting code you don't trust, COI ships a built-in **`hardened`** profile — a one-flag preset:

```bash
coi shell --profile hardened        # restricted net + secret masking + ephemeral + monitoring
coi profile info hardened           # see exactly what it locks down
```

It bundles COI's strongest controls: `network.mode = "restricted"` (no exfil path), workspace secret masking (`.env`, `*.pem`, `secrets/**`, …), host immutability, an **ephemeral** container, **no SSH-agent forwarding**, and real-time threat monitoring with auto-pause/kill. It overrides a weaker global config (a global `mode = "open"` still becomes restricted) and needs no setup.

## Resource and Time Limits

See the [Resource and Time Limits guide](https://github.com/mensfeld/code-on-incus/wiki/Resource-and-Time-Limits) for complete documentation on controlling container resource consumption and runtime.

**Quick example:**
```toml
# ~/.coi/config.toml
[limits.cpu]
count = "2"

[limits.memory]
limit = "2GiB"

[limits.runtime]
max_duration = "2h"
```

**What you can limit:**
- CPU cores and usage percentage
- Memory and swap
- Disk I/O rates
- Maximum runtime and process count
- Auto-stop on time limits


## Container Lifecycle & Session Persistence

See the [Container Lifecycle and Sessions guide](https://github.com/mensfeld/code-on-incus/wiki/Container-Lifecycle-and-Sessions) for detailed explanation of how containers and sessions work.

**Key concepts:**
- **Workspace files**: Always saved (regardless of mode)
- **Session data**: Always saved to `~/.coi/sessions-<tool>/`
- **Ephemeral mode** (default): Container deleted after exit, session preserved
- **Persistent mode** (`[container] persistent = true` in config or a profile): Container kept with all installed packages
- **Resume** (`--resume`): Restore AI conversation in fresh/existing container

**Quick reference:**
```bash
coi shell --resume            # Resume previous conversation
coi attach                    # Reconnect to running container
coi persist <container>       # Convert a running ephemeral session to persistent
coi unfreeze <name>           # Unfreeze paused/frozen container
coi unfreeze                  # Unfreeze all frozen COI containers
close                         # Properly stop container (inside, safe alias for poweroff)
coi shutdown <name>           # Graceful stop (outside)
coi close <name>              # Alias for 'coi shutdown' (deletes it — even a persistent one)
```

## Network Isolation

See the [Network Isolation guide](https://github.com/mensfeld/code-on-incus/wiki/Network-Isolation) for complete documentation on network security and nftables-based network filtering.

**Network modes:**
- **Restricted (default)** - Blocks private networks, allows internet
- **Allowlist** - Only specific domains/IPs allowed
- **Open** - No restrictions (trusted projects only)

```toml
# ~/.coi/config.toml
[network]
mode = "restricted"   # Default — blocks private networks, allows internet
# mode = "allowlist"  # Only specific domains/IPs allowed
# mode = "open"       # No restrictions (trusted projects only)
```

### Allowlist mode

In allowlist mode the container does not resolve names itself. COI resolves the
allowlisted hostnames on the host, installs those addresses in the firewall, and
writes **the same addresses** into the container's `/etc/hosts`. DNS egress is
then blocked, so that hosts file is the container's only way to turn a name into
an address.

That equality is the whole point: the container cannot reach an address the
firewall has not already been given, because there is nowhere else for an address
to come from. Nothing has to stay running for this to hold — it survives `coi`
exiting, detaching from tmux, or the process being killed.

```toml
[network]
mode = "allowlist"
allowed_domains = [
    "api.anthropic.com",       # exact hostname
    "registry.npmjs.org",
    "10.0.0.0/8",              # IPv4 CIDR — no name resolution involved
    "8.8.8.8",                 # raw IPv4 address
]
```

**Wildcards are not supported, and are rejected rather than quietly mishandled.**
Because each name is resolved up front and written to `/etc/hosts`, there is no
answer to write for `*.example.com` — you cannot know which subdomains will be
asked for. List the exact hostnames, or allow the provider's published IP ranges
as CIDRs.

**Claude via GCP Vertex AI** — list the endpoints, which are enumerable:

```toml
allowed_domains = [
    "us-central1-aiplatform.googleapis.com",   # your region
    "oauth2.googleapis.com",
    "sts.googleapis.com",
]
```

Or, for blanket coverage without naming endpoints, use Google's published ranges
(from `https://www.gstatic.com/ipranges/goog.json`) — these need no resolution at
all:

```toml
allowed_domains = ["142.250.0.0/15", "172.217.0.0/16", "216.239.32.0/19"]
```

A host outside the allowlist has no address in `/etc/hosts` and no route through
the firewall, so it fails to resolve and fails to connect.

## Security Monitoring

COI includes **built-in security monitoring** to detect and respond to malicious behavior in real-time:

```toml
# Enable in config (~/.coi/config.toml)
[monitoring]
enabled = true
```

**Protects against:**
- **Reverse shells** - Detects common reverse shell patterns (auto-kill)
- **Data exfiltration** - Monitors large workspace reads/writes (auto-pause)
- **Environment scanning** - Flags processes searching for API keys and secrets
- **Network threats (NFT)** - Kernel-level detection of C2 connections, private network access, DNS tunneling, and allowlist violations

**Automated response levels:**
- **INFO/WARNING**: Logged (+ alert for WARNING)
- **HIGH**: Container **paused** (requires `coi unfreeze` to continue)
- **CRITICAL**: Container **killed immediately**

Audit logs are stored at `~/.coi/audit/<container-name>.jsonl` in JSON Lines format.

`coi audit` streams this log to stdout as JSON Lines (dump, or `--follow` for live in-container events), ready to pipe into a SIEM or `jq`. See the [Security Monitoring wiki page](https://github.com/mensfeld/code-on-incus/wiki/Security-Monitoring) for monitoring commands, configuration, and NFT setup, and the [Audit Log wiki page](https://github.com/mensfeld/code-on-incus/wiki/Audit-Log) for the event format, field reference, sources, and tuning.

## Security Best Practices

See the [Security Best Practices guide](https://github.com/mensfeld/code-on-incus/wiki/Security-Best-Practices) for detailed security recommendations.

COI automatically mounts security-sensitive paths as **read-only** to prevent supply-chain attacks:
- `.git/hooks`, `.git/config`, `.husky`, `.vscode`, `.coi`, `.claude/settings.json`, `.claude/settings.local.json`

The `.claude/settings.*` files can carry auto-executing hooks, so making them read-only stops a contained agent from planting a hook that a later session (or a native run on the host) would auto-execute on open. To opt a path back out, set `[security] writable_paths = [".claude/settings.json"]` in **trusted-scope** config (`~/.coi/config.toml` or `$COI_CONFIG`) — an untrusted project `.coi/config.toml` cannot remove protections. (`[git] writable_hooks = true` remains as a shorthand for `.git/hooks`.) See the wiki for details.

## System Health Check

See the [System Health Check guide](https://github.com/mensfeld/code-on-incus/wiki/System-Health-Check) for detailed information on diagnostics and what's checked.

**Run diagnostics:**
```bash
coi health                    # Basic health check
coi health --format json      # JSON output
coi health --verbose          # Additional checks
```

**What it checks:** System info, kernel version, Incus setup, permissions, security posture (seccomp/AppArmor), privileged container detection, network configuration, storage, monitoring prerequisites, and running containers.

**Exit codes:** 0 (healthy), 1 (degraded), 2 (unhealthy)

## Troubleshooting

See the [Troubleshooting guide](https://github.com/mensfeld/code-on-incus/wiki/Troubleshooting) for common issues and solutions.

**Common issues:**
- **DNS issues during build** - COI automatically fixes systemd-resolved conflicts
- Run `coi health` to diagnose setup problems
- Check the troubleshooting guide for detailed solutions

### Where did the time go?

Set `COI_TIMING_DEBUG=1` on any command to get a wall-clock breakdown on stderr when it
exits: every pipeline phase, every teardown, and every `incus` subprocess, nested
by containment and followed by per-category totals. Almost all of a session's
startup is one `incus` call after another, so this shows exactly which one.

```bash
COI_TIMING_DEBUG=1 coi run -- true          # timeline + totals to stderr
COI_TIMING_DEBUG_JSON=/tmp/run.json coi run -- true   # machine-readable, no stderr noise
scripts/bench-run.py -n 5             # median over N runs, bucketed
```

The most common culprit is an Incus storage pool on the `dir` driver: with no
copy-on-write, every launch re-unpacks the full image (~5-6s per unpacked GB,
so ~18s for a 3 GB image). `coi health` flags this; fix it by recreating the
pool with a CoW driver (zfs/btrfs) — re-running `install.sh` sets one up.
Image size is a per-session cost on such pools, so lean images pay off twice.

Nothing is recorded unless one of those variables is set.

## Frequently Asked Questions

See the [FAQ](https://github.com/mensfeld/code-on-incus/wiki/FAQ) for answers to common questions.

**Topics covered:**
- Orphaned nftables/iptables rules
- How COI compares to Docker Sandboxes and DevContainers
- Windows support (WSL2)
- Security model and prompt injection protection
- API key security and trust model
- What is Incus? (vs tmux)

## Getting Help

- **Slack**: [Join the COI community on Slack](https://slack.karafka.io) — ask questions, report issues, share feedback
- **GitHub Issues**: [Open an issue](https://github.com/mensfeld/code-on-incus/issues) for bug reports and feature requests
- **Wiki**: Browse the [documentation wiki](https://github.com/mensfeld/code-on-incus/wiki) for guides and reference
