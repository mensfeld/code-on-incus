<p align="center">
  <img src="misc/logo.png" alt="Code on Incus Logo" width="350">
</p>

# code-on-incus (`coi`)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mensfeld/code-on-incus)](https://golang.org/)
[![Latest Release](https://img.shields.io/github/v/release/mensfeld/code-on-incus)](https://github.com/mensfeld/code-on-incus/releases)

**Secure and Fast Container Runtime for AI Coding Tools on Linux and macOS**

Run AI coding assistants (Claude Code, Aider, and more) in isolated, production-grade Incus containers with zero permission headaches, perfect file ownership, and true multi-session support.

**Limited Blast Radius:** Prepare your workspace upfront, let the AI agent run in isolation, validate the outcome. No SSH keys, no environment variables, no credentials exposed. If compromised, damage is contained to your workspace. Network isolation helps prevent data exfiltration. Your host system stays protected.

**Security First:** Unlike Docker or bare-metal execution, your environment variables, SSH keys, and Git credentials are **never** exposed to AI tools. Containers run in complete isolation with no access to your host credentials unless explicitly mounted.

*Think Docker for AI coding tools, but with system containers that actually work like real machines.*

![Demo](misc/demo.gif)

## Table of Contents

- [Supported AI Coding Tools](#supported-ai-coding-tools)
- [Features](#features)
- [Quick Start](#quick-start)
- [Why Incus Instead of Docker or Docker Sandboxes?](#why-incus-instead-of-docker-or-docker-sandboxes)
- [Installation](#installation)
- [Usage](#usage)
- [Session Resume](#session-resume)
- [Persistent Mode](#persistent-mode)
- [Configuration](#configuration)
- [System Health Check](https://github.com/mensfeld/code-on-incus/wiki/System-Health-Check)
- [Container Lifecycle & Session Persistence](https://github.com/mensfeld/code-on-incus/wiki/Container-Lifecycle-and-Sessions)
- [Network Isolation](https://github.com/mensfeld/code-on-incus/wiki/Network-Isolation)
- [Resource and Time Limits](https://github.com/mensfeld/code-on-incus/wiki/Resource-and-Time-Limits)
- [Security Best Practices](https://github.com/mensfeld/code-on-incus/wiki/Security-Best-Practices)
- [Troubleshooting](https://github.com/mensfeld/code-on-incus/wiki/Troubleshooting)
- [FAQ](https://github.com/mensfeld/code-on-incus/wiki/FAQ)

## Supported AI Coding Tools

Currently supported:
- **Claude Code** (default) - Anthropic's official CLI tool

Coming soon:
- Aider - AI pair programming in your terminal
- Cursor - AI-first code editor
- And more...

The tool abstraction layer makes it easy to add support for new AI coding assistants.

## Features

**Core Capabilities**
- Multi-slot support - Run parallel AI coding sessions for the same workspace with full isolation
- Session resume - Resume conversations with full history and credentials restored (workspace-scoped)
- Persistent containers - Keep containers alive between sessions (installed tools preserved)
- Workspace isolation - Each session mounts your project directory
- Slot isolation - Each parallel slot has its own home directory (files don't leak between slots)
- Workspace files persist even in ephemeral mode** - Only the container is deleted, your work is always saved
- Container snapshots - Create checkpoints, rollback changes, and branch experiments with full state preservation

**Security & Isolation**
- Automatic UID mapping - No permission hell, files owned correctly
- System containers - Full security isolation, better than Docker privileged mode
- Project separation - Complete isolation between workspaces
- Credential protection - No risk of SSH keys, `.env` files, or Git credentials being exposed to AI tools
- Network isolation - Built-in firewalld limits block private network access and prevent data exfiltration (restricted/allowlist modes)

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

# That's it! Your AI coding assistant is now running in an isolated container with:
# - Your project mounted at /workspace
# - Correct file permissions (no more chown!)
# - Full Docker access inside the container
# - GitHub CLI available for PR/issue management
# - All workspace changes persisted automatically
# - No access to your host SSH keys, env vars, or credentials
```


## Why Incus Instead of Docker or Docker Sandboxes?

### What is Incus?

Incus is a modern Linux container and virtual machine manager, forked from LXD. Unlike Docker (which uses application containers), Incus provides **system containers** that behave like lightweight VMs with full init systems.

### Why Incus Instead of Docker Sandboxes?

- **Linux-first, not Linux-last.** Docker Sandboxes' microVM isolation is only available on macOS and Windows. Linux gets a legacy container-based fallback. COI is built for Linux from the ground up because Incus is Linux-native.

- **No Docker Desktop required.** Docker Sandboxes is a Docker Desktop feature. Docker Desktop is not open source and has commercial licensing requirements for larger organizations. COI depends only on Incus - fully open source, no vendor lock-in, no additional runtime.

- **System containers, not containers-in-VMs.** Incus system containers run a full OS with systemd and native Docker support inside - one clean isolation layer. Docker Sandboxes nests application containers inside microVMs, adding architectural complexity.

- **No permission hell.** Incus automatic UID/GID shifting means files created by agents have correct ownership on the host. No `chown`, no mapping hacks.

- **Credential isolation by default.** Host environment variables, SSH keys, and Git credentials are never exposed to AI tools unless explicitly mounted.

- **Simple and transparent.** No separate daemon, no opaque VM nesting. COI talks directly to Incus - easy to inspect, debug, and extend.

### Key Differences from Docker

| Feature | **code-on-incus (Incus)** | Docker |
|---------|---------------------------|--------|
| **Container Type** | System containers (full OS) | Application containers |
| **Init System** | Full systemd/init | No init (single process) |
| **UID Mapping** | Automatic UID shifting | Manual mapping required |
| **Security** | Unprivileged by default | Often requires privileged mode |
| **File Permissions** | Preserved (UID shifting) | Host UID conflicts |
| **Startup Time** | ~1-2 seconds | ~0.5-1 second |
| **Docker-in-Container** | Native support | Requires DinD hacks |

### Benefits

- **No Permission Hell** - Incus automatically maps container UIDs to host UIDs. Files created by AI tools in-container have correct ownership on host. No `chown` needed.

- **True Isolation** - Full system container means AI tools can run Docker, systemd services, etc. Safer than Docker's privileged mode.

- **Persistent State** - System containers can be stopped/started without data loss. Ideal for long-running AI coding sessions.

- **Resource Efficiency** - Share kernel like Docker, lower overhead than VMs, better density for parallel sessions.

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

### Manual Installation

For users who prefer to verify each step or cannot use the automated installer:

**Prerequisites:**

1. **Linux OS** - Only Linux is supported (Incus is Linux-only)
   - Supported architectures: x86_64/amd64, aarch64/arm64

2. **Incus installed and initialized**

   **Ubuntu/Debian:**
   ```bash
   sudo apt update
   sudo apt install -y incus
   ```

   **Arch/Manjaro:**
   ```bash
   sudo pacman -S incus

   # Enable and start the service (not auto-started on Arch)
   sudo systemctl enable --now incus.socket

   # Configure idmap for unprivileged containers
   echo "root:1000000:1000000000" | sudo tee -a /etc/subuid
   echo "root:1000000:1000000000" | sudo tee -a /etc/subgid
   sudo systemctl restart incus.service
   ```

   See [Incus installation guide](https://linuxcontainers.org/incus/docs/main/installing/) for other distributions.

   **Initialize Incus (all distros):**
   ```bash
   sudo incus admin init --auto
   ```

3. **User in incus-admin group**
   ```bash
   sudo usermod -aG incus-admin $USER
   # Log out and back in for group changes to take effect
   ```

**Installation Steps:**

1. **Download the binary** for your platform:
   ```bash
   # For x86_64/amd64
   curl -fsSL -o coi https://github.com/mensfeld/code-on-incus/releases/latest/download/coi-linux-amd64

   # For aarch64/arm64
   curl -fsSL -o coi https://github.com/mensfeld/code-on-incus/releases/latest/download/coi-linux-arm64
   ```

2. **Verify the download** (optional but recommended):
   ```bash
   # Check file size and type
   ls -lh coi
   file coi
   ```

3. **Install the binary**:
   ```bash
   chmod +x coi
   sudo mv coi /usr/local/bin/
   sudo ln -sf /usr/local/bin/coi /usr/local/bin/claude-on-incus
   ```

4. **Verify installation**:
   ```bash
   coi --version
   ```

**Alternative: Build from Source**

If you prefer to build from source or need a specific version:

```bash
# Prerequisites: Go 1.24.4 or later
git clone https://github.com/mensfeld/code-on-incus.git
cd code-on-incus
make build
sudo make install
```

**Post-Install Setup:**

1. **Optional: Set up ZFS for instant container creation**
   ```bash
   # Install ZFS
   # Ubuntu/Debian (may not be available for all kernels):
   sudo apt-get install -y zfsutils-linux

   # Arch/Manjaro (replace 617 with your kernel version from uname -r):
   # sudo pacman -S linux617-zfs zfs-utils

   # Create ZFS storage pool (50GiB)
   sudo incus storage create zfs-pool zfs size=50GiB

   # Configure default profile to use ZFS
   incus profile device set default root pool=zfs-pool
   ```

   This reduces container startup time from 5-10s to ~50ms. If ZFS is not available, containers will use default storage (slower but fully functional).

2. **Verify group membership** (must be done in a new shell/login):
   ```bash
   groups | grep incus-admin
   ```

**Troubleshooting:**

- **"Permission denied" errors**: Ensure you're in the `incus-admin` group and have logged out/in
- **"incus: command not found"**: Install Incus following the [official guide](https://linuxcontainers.org/incus/docs/main/installing/)
- **Cannot download binary**: Check your internet connection and GitHub access, or build from source

### Build Images

```bash
# Build the unified coi image (5-10 minutes)
coi build

# Custom image from your own build script
coi build custom my-rust-image --script build-rust.sh
coi build custom my-image --base coi --script setup.sh
```

**What's included in the `coi` image:**
- Ubuntu 22.04 base
- Docker (full Docker-in-container support)
- Node.js 20 + npm
- Claude Code CLI (default AI tool)
- GitHub CLI (`gh`)
- tmux for session management
- Common build tools (git, curl, build-essential, etc.)

**Custom images:** Build your own specialized images using build scripts that run on top of the base `coi` image.

## macOS Support

**✅ COI works on macOS** using [Colima](https://github.com/abiosoft/colima) or [Lima](https://github.com/lima-vm/lima) VMs.

See the [macOS Setup Guide](https://github.com/mensfeld/code-on-incus/wiki/macOS-Setup-Guide) for complete instructions including:
- Colima/Lima installation and setup
- Automatic environment detection
- Network configuration (`--network=open` required)
- AWS Bedrock setup for macOS users

**Quick start:**
```bash
brew install colima
colima start --cpu 4 --memory 8 --disk 50
colima ssh
# Follow installation steps in the guide
```

## Usage

### Basic Commands

```bash
# Interactive session (defaults to Claude Code)
coi shell

# Persistent mode - keep container between sessions
coi shell --persistent

# Use specific slot for parallel sessions
coi shell --slot 2

# Resume previous session (auto-detects latest for this workspace)
coi shell --resume

# Resume specific session by ID
coi shell --resume=<session-id>

# Attach to existing session
coi attach

# List active containers and saved sessions
coi list --all

# Gracefully shutdown specific container (60s timeout)
coi shutdown coi-abc12345-1

# Shutdown with custom timeout
coi shutdown --timeout=30 coi-abc12345-1

# Shutdown all containers
coi shutdown --all

# Force kill specific container (immediate)
coi kill coi-abc12345-1

# Kill all containers
coi kill --all

# Cleanup stopped/orphaned containers
coi clean

# Execute commands in containers with PTY support
coi container exec mycontainer -t -- bash        # Interactive shell with PTY
coi container exec mycontainer -- echo "hello"   # Non-interactive command
```

### Global Flags

```bash
--workspace PATH       # Workspace directory to mount (default: current directory)
--slot NUMBER          # Slot number for parallel sessions (0 = auto-allocate)
--persistent           # Keep container between sessions
--resume [SESSION_ID]  # Resume from session (omit ID to auto-detect latest for workspace)
--continue [SESSION_ID] # Alias for --resume
--profile NAME         # Use named profile
--image NAME           # Use custom image (default: coi)
--env KEY=VALUE        # Set environment variables
--storage PATH         # Mount persistent storage
```

### Advanced Usage

See the wiki for detailed documentation on advanced features:

- **[Container Operations](https://github.com/mensfeld/code-on-incus/wiki/Container-Operations)** - Container management and low-level operations
- **[File Transfer](https://github.com/mensfeld/code-on-incus/wiki/File-Transfer)** - Push/pull files between host and containers
- **[Tmux Automation](https://github.com/mensfeld/code-on-incus/wiki/Tmux-Automation)** - Automate AI sessions with tmux commands
- **[Image Management](https://github.com/mensfeld/code-on-incus/wiki/Image-Management)** - Create and manage custom images

### Snapshot Management

See the [Snapshot Management guide](https://github.com/mensfeld/code-on-incus/wiki/Snapshot-Management) for complete documentation on snapshots.

**Quick reference:**
```bash
coi snapshot create checkpoint-1    # Create named snapshot
coi snapshot list                   # List snapshots
coi snapshot restore checkpoint-1   # Restore (container must be stopped)
coi snapshot delete checkpoint-1    # Delete snapshot
```

## Session Resume

Session resume allows you to continue a previous AI coding session with full history and credentials restored.

**Usage:**
```bash
# Auto-detect and resume latest session for this workspace
coi shell --resume

# Resume specific session by ID
coi shell --resume=<session-id>

# Alias: --continue works the same
coi shell --continue

# List available sessions
coi list --all
```

**What's Restored:**
- Full conversation history from previous session
- Tool credentials and authentication (no re-authentication needed)
- User settings and preferences
- Project context and conversation state

**How It Works:**
- After each session, tool state directory (e.g., `.claude`) is automatically saved to `~/.coi/sessions-<tool>/`
- On resume, session data is restored to the container before the tool starts
- Fresh credentials are injected from your host config directory
- The AI tool automatically continues from where you left off

**Workspace-Scoped Sessions:**
- `--resume` only looks for sessions from the **current workspace directory**
- Sessions from other workspaces are never considered (security feature)
- This prevents accidentally resuming a session with a different project context
- Each workspace maintains its own session history

**Note:** Resume works for both ephemeral and persistent containers. For ephemeral containers, the container is recreated but the conversation continues seamlessly.

## Persistent Mode

By default, containers are **ephemeral** (deleted on exit). Your **workspace files always persist** regardless of mode.

Enable **persistent mode** to also keep the container and its installed packages:

**Via CLI:**
```bash
coi shell --persistent
```

**Via config (recommended):**
```toml
# ~/.config/coi/config.toml
[defaults]
persistent = true
```

**Benefits:**
- Install once, use forever - `apt install`, `npm install`, etc. persist
- Faster startup - Reuse existing container instead of rebuilding
- Build artifacts preserved - No re-compiling on each session

**Coding Machines Concept:**

Think of persistent containers as dedicated coding machines owned by the AI agents. The agent can freely install software, configure tools, modify the environment—it's their machine. Your workspace is mounted into their machine, they do the work, and you get the results back. This autonomy lets agents work efficiently without repeatedly setting up their environment, while your host system stays protected.

**What persists:**
- **Ephemeral mode:** Workspace files + session data (container deleted)
- **Persistent mode:** Workspace files + session data + container state + installed packages, system setup

## Configuration

Config file: `~/.config/coi/config.toml`

```toml
[defaults]
image = "coi"
persistent = true
mount_claude_config = true

[tool]
name = "claude"  # AI coding tool to use (currently supports: claude)
# binary = "claude"  # Optional: override binary name

[paths]
# Note: sessions_dir is deprecated - tool-specific dirs are now used automatically
# (e.g., ~/.coi/sessions-claude/, ~/.coi/sessions-aider/)
sessions_dir = "~/.coi/sessions"  # Legacy path (not used for new sessions)
storage_dir = "~/.coi/storage"

[incus]
project = "default"
group = "incus-admin"
claude_uid = 1000

[profiles.rust]
image = "coi-rust"
environment = { RUST_BACKTRACE = "1" }
persistent = true
```

**Configuration hierarchy** (highest precedence last):
1. Built-in defaults
2. System config (`/etc/coi/config.toml`)
3. User config (`~/.config/coi/config.toml`)
4. Project config (`./.coi.toml`)
5. CLI flags


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
sudo poweroff                 # Properly stop container (inside)
coi shutdown <name>           # Graceful stop (outside)
```

## Network Isolation

See the [Network Isolation guide](https://github.com/mensfeld/code-on-incus/wiki/Network-Isolation) for complete documentation on network security and firewalld setup.

**Network modes:**
- **Restricted (default)** - Blocks private networks, allows internet
- **Allowlist** - Only specific domains/IPs allowed
- **Open** - No restrictions (trusted projects only)

**Quick examples:**
```bash
coi shell                      # Restricted mode (default)
coi shell --network=allowlist  # Allowlist mode
coi shell --network=open       # Open mode
```

**Accessing container services from host:**
```bash
coi list  # Get container IP
curl http://<container-ip>:3000
```

**Note:** Network isolation requires firewalld. Use `--network=open` or see the guide for firewalld setup instructions.

## Security Best Practices

See the [Security Best Practices guide](https://github.com/mensfeld/code-on-incus/wiki/Security-Best-Practices) for detailed security recommendations.

**Key practice - Disable git hooks when committing AI-generated code:**
```bash
# Commit with hooks disabled
git -c core.hooksPath=/dev/null commit --no-verify -m "your message"

# Or create an alias
alias gcs='git -c core.hooksPath=/dev/null commit --no-verify'
```

**Why:** Containers can write to `.git/` in your workspace. This prevents potentially modified hooks from executing when you commit from your host.

## System Health Check

See the [System Health Check guide](https://github.com/mensfeld/code-on-incus/wiki/System-Health-Check) for detailed information on diagnostics and what's checked.

**Run diagnostics:**
```bash
coi health                    # Basic health check
coi health --format json      # JSON output
coi health --verbose          # Additional checks
```

**What it checks:** System info, Incus setup, permissions, network configuration, storage, and running containers.

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
- How COI compares to Docker Sandboxes and DevContainers
- Windows support (WSL2)
- Security model and prompt injection protection
- API key security and trust model
- What is Incus? (vs tmux)

