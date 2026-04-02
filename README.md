<p align="center">
  <img src="misc/logo.png" alt="Code on Incus Logo" width="350">
</p>

# code-on-incus (`coi`)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mensfeld/code-on-incus)](https://golang.org/)
[![Latest Release](https://img.shields.io/github/v/release/mensfeld/code-on-incus)](https://github.com/mensfeld/code-on-incus/releases)

**Security-Hardened Container Runtime for AI Coding Agents with Real-Time Threat Detection**

Run AI coding assistants (Claude Code, opencode, Aider, and more) in isolated, production-grade Incus containers with zero permission headaches, perfect file ownership, and true multi-session support.

**Limited Blast Radius:** Prepare your workspace upfront, let the AI agent run in isolation, validate the outcome. No SSH keys, no environment variables, no credentials exposed. If compromised, damage is contained to your workspace. Network isolation helps prevent data exfiltration. Your host system stays protected.

**Security First:** Unlike Docker or bare-metal execution, your environment variables, SSH keys, and Git credentials are **never** exposed to AI tools. Containers run in complete isolation with no access to your host credentials unless explicitly mounted.

**Proactive Defense:** COI doesn't just isolate AI tools — it can actively watch them. Enable the built-in security monitoring daemon (`--monitor`) to detect reverse shells, credential scanning, and large data reads in real time, automatically pausing or killing the container before damage can occur. No manual intervention needed.

*Think Docker for AI coding tools, but with system containers that actually work like real machines.*

<p align="center">
  <a href="https://www.youtube.com/watch?v=t78-JUnTK5Q">
    <img src="https://img.youtube.com/vi/t78-JUnTK5Q/maxresdefault.jpg" alt="BetterStack video about Code on Incus" width="600">
  </a>
  <br>
  <em>Watch the BetterStack video about Code on Incus</em>
</p>

![Demo](misc/demo.gif)

## Table of Contents

- [Supported AI Coding Tools](#supported-ai-coding-tools)
- [Supported Tools (detailed)](https://github.com/mensfeld/code-on-incus/wiki/Supported-Tools)
- [Features](#features)
- [Quick Start](#quick-start)
- [Why Incus Instead of Docker or Docker Sandboxes?](#why-incus-instead-of-docker-or-docker-sandboxes)
- [Installation](#installation)
- [macOS Support](#macos-support)
- [Usage](#usage)
- [Session Resume](#session-resume)
- [Persistent Mode](#persistent-mode)
- [Configuration](#configuration)
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

Coming soon:
- Aider - AI pair programming in your terminal
- Cursor - AI-first code editor
- And more...

**Tool selection:**
```bash
coi shell                    # Uses default tool (Claude Code)
coi shell --tool opencode    # Use opencode instead
```

**Permission mode** - Control whether AI tools run autonomously or ask before each action:
```toml
# ~/.config/coi/config.toml or .coi/config.toml
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
- SSH agent forwarding - Use git-over-SSH inside containers without copying private keys (`--ssh-agent` or `[ssh] forward_agent = true`)
- Environment variable forwarding - Selectively forward host env vars by name (`--forward-env` or `forward_env` in config)
- Host timezone inheritance - Containers automatically inherit the host's timezone (configurable via `--timezone` or `[timezone]` config)
- Sandbox context file - Auto-injected `~/SANDBOX_CONTEXT.md` tells AI tools about their environment (network mode, workspace path, persistence, etc.). Automatically loaded into each tool's native context system: Claude Code via `~/.claude/CLAUDE.md`, OpenCode via the `instructions` field in `opencode.json` (opt out with `auto_context = false`)

**Security & Isolation**
- Credential protection - SSH keys, `.env` files, Git credentials, and environment variables are **never** exposed unless explicitly mounted
- Privileged container guard - Refuses to start when `security.privileged=true` is detected, which defeats all container isolation
- Security posture verification - `coi health` checks seccomp, AppArmor, and privilege settings to confirm full isolation
- Kernel version enforcement - Warns on host kernels below 5.15 that may lack security features for safe isolation
- Real-time threat detection - Kernel-level nftables monitoring detects reverse shells, C2 connections, data exfiltration, DNS tunneling, and credential scanning
- Automated response - Auto-pause on HIGH threats, auto-kill on CRITICAL — no manual intervention needed
- Network isolation - Firewalld-based restricted/allowlist/open modes block private network access and prevent exfiltration
- Protected paths - `.git/hooks`, `.git/config`, `.husky`, `.vscode` mounted read-only to prevent supply-chain attacks
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

# Or use opencode instead
coi shell --tool opencode

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
| **Network isolation** | Firewalld (3 modes) | Basic | No |
| **Protected paths** | Read-only mounts | No | No |
| **Auto response (pause/kill)** | Yes | No | No |
| **Audit logging** | JSONL forensics | No | No |
| **Supply-chain attack prevention** | Git hooks/IDE configs protected | No | No |

### Why Incus Instead of Docker Sandboxes?

- **Linux-first, not Linux-last.** Docker Sandboxes' microVM isolation is only available on macOS and Windows. Linux gets a legacy container-based fallback. COI is built for Linux from the ground up because Incus is Linux-native.

- **No Docker Desktop required.** Docker Sandboxes is a Docker Desktop feature. Docker Desktop is not open source and has commercial licensing requirements for larger organizations. COI depends only on Incus - fully open source, no vendor lock-in, no additional runtime.

- **System containers, not containers-in-VMs.** Incus system containers run a full OS with systemd and native Docker support inside - one clean isolation layer. Docker Sandboxes nests application containers inside microVMs, adding architectural complexity.

- **No permission hell.** Incus automatic UID/GID shifting means files created by agents have correct ownership on the host. No `chown`, no mapping hacks.

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

**Manual installation:** Download the binary from [GitHub Releases](https://github.com/mensfeld/code-on-incus/releases), make it executable, and move to `/usr/local/bin/`. Requires Linux with Incus installed and user in the `incus-admin` group. See the [Incus installation guide](https://linuxcontainers.org/incus/docs/main/installing/) for setting up Incus.

### Build Images

```bash
# Build the unified coi image (5-10 minutes)
coi build

# Build without compression (faster iteration)
coi build --compression none

# Custom image from your own build script
coi build custom my-rust-image --script build-rust.sh
coi build custom my-image --base coi --script setup.sh
```

**What's included in the `coi` image:**
- Ubuntu 22.04 base with Docker (full Docker-in-container support)
- **mise** (polyglot runtime manager) — Python 3, pnpm, TypeScript, tsx pre-installed; add more with `mise use go@latest`, `mise use ruby@3`, etc.
- Node.js 20 LTS (system, for Claude CLI) + npm
- Claude Code CLI (default AI tool) + GitHub CLI (`gh`)
- tmux, git, curl, build-essential, and common build tools
- Modern CLI utilities: fd-find, bat, tree
- Debugging tools: strace, lsof
- Database clients: sqlite3, postgresql-client, redis-tools
- imagemagick for image processing

**Custom images:** Build your own specialized images using build scripts that run on top of the base `coi` image.

## macOS Support

**COI works on macOS** using [Colima](https://github.com/abiosoft/colima) or [Lima](https://github.com/lima-vm/lima) VMs. See the [macOS Setup Guide](https://github.com/mensfeld/code-on-incus/wiki/macOS-Setup-Guide) for complete instructions.

## Usage

### Basic Commands

```bash
# Interactive session (defaults to Claude Code)
coi shell

# Use a different AI tool
coi shell --tool opencode

# Persistent mode - keep container between sessions
coi shell --persistent

# Use specific slot for parallel sessions
coi shell --slot 2

# Enable security monitoring
coi shell --monitor

# Resume previous session
coi shell --resume

# Run a command in an ephemeral container
coi run "npm test"

# Attach to existing session
coi attach

# List active containers and saved sessions
coi list --all

# Gracefully shutdown / force kill containers
coi shutdown coi-abc12345-1
coi kill --all

# Cleanup stopped containers and orphaned resources
coi clean
```

### Global Flags

```bash
--workspace PATH        # Workspace directory to mount (default: current directory)
--slot NUMBER           # Slot number for parallel sessions (0 = auto-allocate)
--persistent            # Keep container between sessions
--resume [SESSION_ID]   # Resume from session (omit ID to auto-detect latest for workspace)
--continue [SESSION_ID] # Alias for --resume
--profile NAME          # Use named profile
--image NAME            # Use custom image (default: coi)
--env KEY=VALUE         # Set environment variables (repeatable)
--mount HOST:CONTAINER  # Mount directory into container (repeatable)
--network MODE          # Network mode: restricted (default), allowlist, open
--ssh-agent             # Forward host SSH agent into container (git over SSH without copying keys)
--forward-env VARS      # Forward host environment variables by name (comma-separated)
--monitor               # Enable security monitoring with automatic threat response
--timezone ZONE         # Container timezone: host (default), utc, or IANA name (e.g., Europe/Warsaw)
--compression ALG       # Compression for build/publish: none, gzip, xz (default: gzip)
--writable-git-hooks    # Allow container to write to .git/hooks (disables protection)
```

### Advanced Usage

See the wiki for detailed documentation:

- **[Container Operations](https://github.com/mensfeld/code-on-incus/wiki/Container-Operations)** - Container management and low-level operations
- **[File Transfer](https://github.com/mensfeld/code-on-incus/wiki/File-Transfer)** - Push/pull files between host and containers
- **[Tmux Automation](https://github.com/mensfeld/code-on-incus/wiki/Tmux-Automation)** - Automate AI sessions with tmux commands
- **[Image Management](https://github.com/mensfeld/code-on-incus/wiki/Image-Management)** - Create and manage custom images
- **[Snapshot Management](https://github.com/mensfeld/code-on-incus/wiki/Snapshot-Management)** - Create checkpoints and rollback changes

## Session Resume

Resume a previous AI coding session with full history and credentials restored:

```bash
coi shell --resume              # Auto-detect latest session for this workspace
coi shell --resume=<session-id> # Resume specific session
coi list --all                  # List available sessions
```

**What's restored:** Full conversation history, tool credentials, user settings, and project context. Sessions are workspace-scoped — `--resume` only finds sessions from the current workspace directory.

See the [Container Lifecycle and Sessions guide](https://github.com/mensfeld/code-on-incus/wiki/Container-Lifecycle-and-Sessions) for details on how session persistence works.

## Persistent Mode

By default, containers are **ephemeral** (deleted on exit). Your **workspace files always persist** regardless of mode.

Enable **persistent mode** to also keep the container and its installed packages:

```bash
coi shell --persistent
```

```toml
# Or via config (~/.config/coi/config.toml)
[defaults]
persistent = true
```

**What persists:**
- **Ephemeral mode:** Workspace files + session data (container deleted)
- **Persistent mode:** Workspace files + session data + container state + installed packages, system setup

See the [Container Lifecycle and Sessions guide](https://github.com/mensfeld/code-on-incus/wiki/Container-Lifecycle-and-Sessions) for details.

## Configuration

Config file: `~/.config/coi/config.toml`

```toml
[defaults]
image = "coi"
persistent = true

[tool]
name = "claude"
permission_mode = "bypass"
# auto_context = true          # Auto-inject sandbox context into tool's native system
```

**Configuration hierarchy** (highest precedence last):
1. Built-in defaults
2. System config (`/etc/coi/config.toml`)
3. User config (`~/.config/coi/config.toml`)
4. Project config (`./.coi/config.toml`)
5. `COI_CONFIG` environment variable
6. Environment variables (`CLAUDE_ON_INCUS_*`, `COI_*`)
7. CLI flags

Place a `.coi/config.toml` in any repository root to auto-configure COI for that project — useful for teams to share container image, environment, and resource limits.

See the [Configuration wiki page](https://github.com/mensfeld/code-on-incus/wiki/Configuration) for the full config reference, per-repo setup, profiles, and environment variables.

## Profiles

Profiles are reusable container configurations bundling image, tool, limits, mounts, build scripts, and environment into named templates. Each profile is a self-contained directory with its own `config.toml` and optional build script.

```
.coi/profiles/
├── rust-dev/
│   ├── config.toml      # profile config
│   └── build.sh         # profile-specific build script
└── python-ml/
    ├── config.toml
    └── setup.sh
```

Example profile config (`.coi/profiles/rust-dev/config.toml`):

```toml
image = "coi-rust"
persistent = true
forward_env = ["CARGO_HOME"]

[environment]
RUST_BACKTRACE = "1"

[tool]
name = "claude"
permission_mode = "bypass"

[limits.cpu]
count = "4"
```

```bash
# Use a profile
coi shell --profile rust-dev

# List all available profiles
coi profile list

# Show profile details
coi profile show rust-dev
```

Profile directories are scanned at all config levels (`/etc/coi/profiles/`, `~/.config/coi/profiles/`, `.coi/profiles/`).

## Resource and Time Limits

See the [Resource and Time Limits guide](https://github.com/mensfeld/code-on-incus/wiki/Resource-and-Time-Limits) for complete documentation on controlling container resource consumption and runtime.

**Quick example:**
```bash
# Limit CPU, memory, and runtime
coi shell --limit-cpu="2" --limit-memory="2GiB" --limit-duration="2h"
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
- **Persistent mode** (`--persistent`): Container kept with all installed packages
- **Resume** (`--resume`): Restore AI conversation in fresh/existing container

**Quick reference:**
```bash
coi shell --persistent        # Keep container between sessions
coi shell --resume            # Resume previous conversation
coi attach                    # Reconnect to running container
coi persist                   # Convert ephemeral session to persistent
coi resume <name>             # Resume paused/frozen container
sudo poweroff                 # Properly stop container (inside)
coi shutdown <name>           # Graceful stop (outside)
```

## Network Isolation

See the [Network Isolation guide](https://github.com/mensfeld/code-on-incus/wiki/Network-Isolation) for complete documentation on network security and firewalld setup.

**Network modes:**
- **Restricted (default)** - Blocks private networks, allows internet
- **Allowlist** - Only specific domains/IPs allowed
- **Open** - No restrictions (trusted projects only)

```bash
coi shell                      # Restricted mode (default)
coi shell --network=allowlist  # Allowlist mode
coi shell --network=open       # Open mode
```

## Security Monitoring

COI includes **built-in security monitoring** to detect and respond to malicious behavior in real-time:

```bash
coi shell --monitor            # Enable via CLI flag
```

```toml
# Or enable permanently in config
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
- **HIGH**: Container **paused** (requires `coi resume` to continue)
- **CRITICAL**: Container **killed immediately**

Audit logs are stored at `~/.coi/audit/<container-name>.jsonl` in JSON Lines format.

See the [Security Monitoring wiki page](https://github.com/mensfeld/code-on-incus/wiki/Security-Monitoring) for monitoring commands, configuration options, NFT setup, and audit log management.

## Security Best Practices

See the [Security Best Practices guide](https://github.com/mensfeld/code-on-incus/wiki/Security-Best-Practices) for detailed security recommendations.

COI automatically mounts security-sensitive paths as **read-only** to prevent supply-chain attacks:
- `.git/hooks`, `.git/config`, `.husky`, `.vscode`

Use `--writable-git-hooks` to opt out, or customize protected paths via config. See the wiki for details.

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
## Frequently Asked Questions

See the [FAQ](https://github.com/mensfeld/code-on-incus/wiki/FAQ) for answers to common questions.

**Topics covered:**
- Orphaned firewalld zone bindings (Docker + firewalld interaction)
- How COI compares to Docker Sandboxes and DevContainers
- Windows support (WSL2)
- Security model and prompt injection protection
- API key security and trust model
- What is Incus? (vs tmux)
