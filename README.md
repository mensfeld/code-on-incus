<p align="center">
  <img src="misc/logo.png" alt="Code on Incus Logo" width="350">
</p>

# code-on-incus (`coi`)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mensfeld/code-on-incus)](https://golang.org/)
[![Latest Release](https://img.shields.io/github/v/release/mensfeld/code-on-incus)](https://github.com/mensfeld/code-on-incus/releases)

**Secure and Fast Container Runtime for AI Coding Tools on Linux and macOS**

Run AI coding assistants (Claude Code, Aider, and more) in isolated, production-grade Incus containers with zero permission headaches, perfect file ownership, and true multi-session support.

**Limited Blast Radius:** Prepare your workspace upfront, let the AI agent run in isolation, validate the outcome. No SSH keys, no environment variables, no credentials exposed-only the AI vendor API key is needed. If compromised, damage is contained to your workspace. Network isolation helps prevent data exfiltration. Your host system stays protected.

**Security First:** Unlike Docker or bare-metal execution, your environment variables, SSH keys, and Git credentials are **never** exposed to AI tools. Containers run in complete isolation with no access to your host credentials unless explicitly mounted.

*Think Docker for AI coding tools, but with system containers that actually work like real machines.*

![Demo](misc/demo.gif)

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
- **Slot isolation** - Each parallel slot has its own home directory (files don't leak between slots)
- **Workspace files persist even in ephemeral mode** - Only the container is deleted, your work is always saved
- **Container snapshots** - Create checkpoints, rollback changes, and branch experiments with full state preservation

**Security & Isolation**
- Automatic UID mapping - No permission hell, files owned correctly
- System containers - Full security isolation, better than Docker privileged mode
- Project separation - Complete isolation between workspaces
- **Credential protection** - No risk of SSH keys, `.env` files, or Git credentials being exposed to AI tools

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

## Running on macOS (Colima/Lima)

COI can run on macOS by using Incus inside a [Colima](https://github.com/abiosoft/colima) or [Lima](https://github.com/lima-vm/lima) VM. These tools provide Linux VMs on macOS that can run Incus.

**Automatic Environment Detection**: COI automatically detects when running inside a Colima or Lima VM and adjusts its configuration accordingly. No manual configuration needed!

### How It Works

1. **Colima/Lima handle UID mapping** - These VMs mount macOS directories using virtiofs and map UIDs at the VM level
2. **COI detects the environment** - Checks for virtiofs mounts in `/proc/mounts` and the `lima` user
3. **UID shifting is auto-disabled** - COI automatically disables Incus's `shift=true` option to avoid conflicts with VM-level mapping

### Network Mode on macOS

**Important:** Network isolation modes (`restricted`, `allowlist`) require firewalld, which is not available in Colima/Lima VMs by default. Use `--network=open` for unrestricted network access:

```bash
# Inside Colima/Lima VM
coi shell --network=open
```

Or set it as default in your config:

```toml
# ~/.config/coi/config.toml
[network]
mode = "open"
```

### AWS Bedrock on macOS/Colima

If you're using Claude via AWS Bedrock, COI will automatically validate your setup when running in Colima/Lima and prevent startup with helpful error messages if anything is misconfigured.

#### Common Issues and Fixes

**Issue 1: Dual .aws Directory Locations**

Colima creates two `.aws` locations that can get out of sync:
- `/home/lima/.aws/` - Lima's VM home (where `~` expands inside Colima)
- `/Users/<yourname>/.aws/` - macOS home (mounted via virtiofs)

**Solution:** Pick one location and be consistent:
1. **Recommended:** Use macOS path `/Users/<yourname>/.aws/`
2. Run `aws sso login` on your Mac (not inside Colima)
3. Ensure it's mounted to containers (see below)

**Issue 2: Restrictive Permissions on SSO Cache**

AWS SSO creates cache files with permissions `-rw-------` (600), which become unreadable inside containers.

**Solution:** After running `aws sso login`, fix permissions:
```bash
chmod 644 ~/.aws/sso/cache/*.json
```

**Issue 3: .aws Not Mounted**

The container needs access to your AWS credentials.

**Solution:** Add to your config:
```toml
# ~/.config/coi/config.toml
[[mounts.default]]
host = "~/.aws"
container = "/home/code/.aws"
```

#### Complete Bedrock Setup Example

1. Configure Bedrock in `~/.claude/settings.json`:
```json
{
  "anthropic": {
    "apiProvider": "bedrock",
    "bedrock": {
      "region": "us-west-2",
      "profile": "default"
    }
  }
}
```

2. Set up AWS SSO (on macOS, not in Colima):
```bash
aws configure sso
aws sso login
chmod 644 ~/.aws/sso/cache/*.json
```

3. Configure mount in `~/.config/coi/config.toml`:
```toml
[[mounts.default]]
host = "/Users/<yourname>/.aws"
container = "/home/code/.aws"
```

4. Launch COI:
```bash
colima ssh
coi shell --network=open
```

COI will validate your setup and provide clear error messages if anything is missing or misconfigured.

### Setup

```bash
# Install Colima (example)
brew install colima

# Start Colima VM with sufficient resources
colima start --cpu 4 --memory 8 --disk 50

# SSH into the VM
colima ssh

# Inside the VM, install Incus
sudo apt update && sudo apt install -y incus
sudo incus admin init --auto
sudo usermod -aG incus-admin $USER
newgrp incus-admin

# Install COI
curl -fsSL https://raw.githubusercontent.com/mensfeld/code-on-incus/master/install.sh | bash

# Build image and start a session
coi build
coi shell --network=open
```

**Manual Override**: In rare cases where auto-detection doesn't work, you can manually configure:

```toml
# ~/.config/coi/config.toml
[incus]
disable_shift = true
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

### Container Management

```bash
# List all containers and sessions
coi list --all

# Machine-readable JSON output (for programmatic use)
coi list --format=json
coi list --all --format=json

# Output shows container mode:
#   coi-abc12345-1 (ephemeral)   - will be deleted on exit
#   coi-abc12345-2 (persistent)  - will be kept for reuse

# Kill specific container (stop and delete)
coi kill <container-name>

# Kill multiple containers
coi kill <container1> <container2>

# Kill all containers (with confirmation)
coi kill --all

# Kill all without confirmation
coi kill --all --force

# Clean up stopped/orphaned containers
coi clean
coi clean --force  # Skip confirmation
```

### Advanced Container Operations

Low-level container commands for advanced use cases:

```bash
# Launch a new container
coi container launch coi my-container
coi container launch coi my-container --ephemeral

# Start/stop/delete containers
coi container start my-container
coi container stop my-container
coi container stop my-container --force
coi container delete my-container
coi container delete my-container --force

# Execute commands in containers
coi container exec my-container -- ls -la /workspace
coi container exec my-container --user 1000 --env FOO=bar --cwd /workspace -- npm test

# Capture output in different formats
coi container exec my-container --capture -- echo "hello"  # JSON output (default)
coi container exec my-container --capture --format=raw -- pwd  # Raw stdout (for scripting)

# Check container status
coi container exists my-container
coi container running my-container

# Mount directories
coi container mount my-container workspace /home/user/project /workspace --shift
```

### File Transfer

Transfer files and directories between host and containers:

```bash
# Push files/directories into a container
coi file push ./config.json my-container:/workspace/config.json
coi file push -r ./src my-container:/workspace/src

# Pull files/directories from a container
coi file pull my-container:/workspace/build.log ./build.log
coi file pull -r my-container:/root/.claude ./saved-sessions/session-123/
```

### Tmux Automation

Interact with running AI coding sessions for automation workflows:

```bash
# List all active tmux sessions
coi tmux list

# Send commands/prompts to a running session
coi tmux send coi-abc12345-1 "write a hello world script"
coi tmux send coi-abc12345-1 "/exit"

# Capture current output from a session
coi tmux capture coi-abc12345-1
```

**Note:** Sessions use tmux internally, so standard tmux commands work after attaching with `coi attach`.

### Image Management

Advanced image operations:

```bash
# List images with filters
coi image list                           # List COI images
coi image list --all                     # List all local images
coi image list --prefix claudeyard-      # Filter by prefix
coi image list --format json             # JSON output

# Publish containers as images
coi image publish my-container my-custom-image --description "Custom build"

# Delete images
coi image delete my-custom-image

# Check if image exists
coi image exists coi

# Clean up old image versions
coi image cleanup claudeyard-node-42- --keep 3
```

### Snapshot Management

Create container snapshots for checkpointing, rollback, and branching workflows:

```bash
# Create snapshots
coi snapshot create                     # Auto-named snapshot (snap-YYYYMMDD-HHMMSS)
coi snapshot create checkpoint-1        # Named snapshot
coi snapshot create --stateful live     # Include process memory state
coi snapshot create -c coi-abc-1 backup # Specific container

# List snapshots
coi snapshot list                       # Current workspace container
coi snapshot list -c coi-abc-1          # Specific container
coi snapshot list --all                 # All COI containers
coi snapshot list --format json         # JSON output

# Restore from snapshot (requires stopped container)
coi snapshot restore checkpoint-1       # Restore with confirmation
coi snapshot restore checkpoint-1 -f    # Skip confirmation
coi snapshot restore checkpoint-1 --stateful  # Restore with process state

# Delete snapshots
coi snapshot delete checkpoint-1        # Delete specific snapshot
coi snapshot delete --all               # Delete all (with confirmation)
coi snapshot delete --all -f            # Delete all without confirmation

# Show snapshot details
coi snapshot info checkpoint-1          # Text output
coi snapshot info checkpoint-1 --format json  # JSON output
```

**Container Resolution:**
- Uses `--container` flag if provided
- Falls back to `COI_CONTAINER` environment variable
- Auto-resolves from current workspace if exactly one container exists
- Error if multiple containers found (use `--container` to specify)

**Safety Features:**
- Restore requires container to be stopped (`coi container stop <name>`)
- Destructive operations require confirmation (skip with `--force`)
- Snapshots capture complete container state including session data
- Stateful snapshots include process memory for live state preservation

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

**What persists:**
- **Ephemeral mode:** Workspace files + session data (container deleted)
- **Persistent mode:** Workspace files + session data + container state + installed packages

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

Control container resource consumption and runtime with configurable limits. All limits can be set via config file or CLI flags.

### Configuration

Add to your `~/.config/coi/config.toml`:

```toml
[limits.cpu]
count = "2"              # CPU cores: "2", "0-3", "0,1,3" or "" (unlimited)
allowance = "50%"        # CPU time: "50%", "25ms/100ms" or "" (unlimited)
priority = 0             # CPU priority: 0-10 (higher = more priority)

[limits.memory]
limit = "2GiB"           # Memory: "512MiB", "2GiB", "50%" or "" (unlimited)
enforce = "soft"         # Enforcement: "hard" or "soft"
swap = "true"            # Swap: "true", "false", or size like "1GiB"

[limits.disk]
read = "10MiB/s"         # Read rate: "10MiB/s", "1000iops" or "" (unlimited)
write = "5MiB/s"         # Write rate: "5MiB/s", "1000iops" or "" (unlimited)
max = ""                 # Combined I/O limit (overrides read/write)
priority = 0             # Disk priority: 0-10

[limits.runtime]
max_duration = "2h"      # Max runtime: "2h", "30m", "1h30m" or "" (unlimited)
max_processes = 0        # Max processes: 100 or 0 (unlimited)
auto_stop = true         # Auto-stop when max_duration reached
stop_graceful = true     # Graceful (true) vs force (false) stop
```

### CLI Flags

Override config file settings with flags:

```bash
# CPU limits
coi shell --limit-cpu="2" --limit-cpu-allowance="50%"

# Memory limits
coi shell --limit-memory="2GiB" --limit-memory-swap="1GiB"

# Disk limits
coi shell --limit-disk-read="10MiB/s" --limit-disk-write="5MiB/s"

# Runtime limits
coi shell --limit-duration="2h" --limit-processes=100

# Combine multiple limits
coi shell \
  --limit-cpu="2" \
  --limit-memory="2GiB" \
  --limit-duration="1h"
```

### Profile-Specific Limits

Define limits per profile:

```toml
[profiles.limited]
image = "coi"
persistent = false

[profiles.limited.limits.cpu]
count = "2"
allowance = "50%"

[profiles.limited.limits.memory]
limit = "2GiB"

[profiles.limited.limits.runtime]
max_duration = "2h"
auto_stop = true
```

Use with: `coi shell --profile limited`

### Time Limits and Auto-Stop

When `max_duration` is set and `auto_stop = true`:
- Container automatically stops after the specified duration
- Graceful stop preserves session data
- Force stop (`stop_graceful = false`) terminates immediately
- Useful for preventing runaway sessions or managing costs

Example:
```bash
# Auto-stop after 2 hours
coi shell --limit-duration="2h"

# The container will gracefully stop after 2 hours, saving session data
```

### Precedence

Limits are applied with this precedence (highest to lowest):
1. CLI flags (e.g., `--limit-cpu="2"`)
2. Profile limits (if `--profile` specified)
3. Config `[limits]` section
4. Unlimited (Incus defaults)

### Examples

**Limit resources for expensive operations:**
```bash
coi shell \
  --limit-cpu="4" \
  --limit-memory="4GiB" \
  --limit-duration="30m"
```

**Prevent runaway processes:**
```bash
coi shell \
  --limit-processes=100 \
  --limit-duration="1h" \
  --limit-memory="2GiB"
```

**Development profile with limits:**
```toml
[profiles.dev]
image = "coi"
persistent = true

[profiles.dev.limits.cpu]
count = "2"

[profiles.dev.limits.memory]
limit = "4GiB"

[profiles.dev.limits.runtime]
max_duration = "4h"
```


## Container Lifecycle & Session Persistence

Understanding how containers and sessions work in `coi`:

### How It Works Internally

1. **Containers are always launched as non-ephemeral** (persistent in Incus terms)
   - This allows saving session data even if the container is stopped from within (e.g., `sudo shutdown 0`)
   - Session data can be pulled from stopped containers, but not from deleted ones

2. **Inside the container**: `tmux` → `bash` → `<ai-tool>`
   - When the AI tool exits, you're dropped to bash
   - From bash you can: type `exit`, press `Ctrl+b d` to detach, or run `sudo shutdown 0`

3. **On cleanup** (when you exit/detach):
   - Session data (tool config directory) is **always** saved to `~/.coi/sessions-<tool>/`
   - If `--persistent` was NOT set: container is deleted after saving
   - If `--persistent` was set: container is kept for reuse

### What Gets Preserved

| Mode | Workspace Files | AI Tool Session | Container State |
|------|----------------|-----------------|-----------------|
| **Default (ephemeral)** | Always saved | Always saved | Deleted |
| **`--persistent`** | Always saved | Always saved | Kept |

### Session vs Container Persistence

- **`--resume`**: Restores the **AI tool conversation** in a fresh container
  - Use when you want to continue a conversation but don't need installed packages
  - Container is recreated, only tool session data is restored
  - **Workspace-scoped**: Only finds sessions from the current workspace directory (security feature)

- **`--persistent`**: Keeps the **entire container** with all modifications
  - Use when you've installed tools, built artifacts, or modified the environment
  - `coi attach` reconnects to the same container with everything intact

### Stopping Containers

From **inside** the container:
- `exit` in bash → exits bash but keeps container running (use for temporary shell exit)
- `Ctrl+b d` → detaches from tmux, container stays running
- `sudo shutdown 0` or `sudo poweroff` → stops container, session is saved, then container is deleted (or kept if `--persistent`)

From **outside** (host):
- `coi shutdown <name>` → graceful stop with session save, then delete (60s timeout by default)
- `coi shutdown --timeout=30 <name>` → graceful stop with 30s timeout
- `coi shutdown --all` → graceful stop all containers (with confirmation)
- `coi shutdown --all --force` → graceful stop all without confirmation
- `coi kill <name>` → force stop and delete immediately
- `coi kill --all` → force stop and delete all containers (with confirmation)
- `coi kill --all --force` → force stop all without confirmation

### Example Workflows

**Quick task (default mode):**
```bash
coi shell                    # Start session with default AI tool
# ... work with AI assistant ...
sudo poweroff                # Shutdown container → session saved, container deleted
coi shell --resume           # Continue conversation in fresh container
```

**Note:** `exit` in bash keeps the container running - use `sudo poweroff` or `sudo shutdown 0` to properly end the session. Both require sudo but no password.

**Long-running project (`--persistent`):**
```bash
coi shell --persistent       # Start persistent session
# ... install tools, build things ...
# Press Ctrl+b d to detach
coi attach                   # Reconnect to same container with all tools
sudo poweroff                # When done, shutdown and save
coi shell --persistent --resume  # Resume with all installed tools intact
```

**Parallel sessions (multi-slot):**
```bash
# Terminal 1: Start first session (auto-allocates slot 1)
coi shell
# ... working on feature A ...
# Press Ctrl+b d to detach (container stays running)

# Terminal 2: Start second session (auto-allocates slot 2)
coi shell
# ... working on feature B in parallel ...

# Both sessions share the same workspace but have isolated:
# - Home directories (~/slot1_file won't appear in slot 2)
# - Installed packages
# - Running processes
# - AI tool conversation history

# List both running sessions
coi list
#   coi-abc12345-1 (ephemeral)
#   coi-abc12345-2 (ephemeral)

# When done, shutdown all sessions
coi shutdown --all
```

## Network Isolation

COI provides network isolation to protect your host and private networks from container access.

**Requirements:** Network isolation (restricted/allowlist modes) requires **firewalld** to be installed and running. COI uses firewalld direct rules to filter container traffic in the FORWARD chain. If firewalld is not available, you'll need to use `--network=open` or install and configure firewalld. See [Firewalld Setup](#firewalld-setup) below for instructions.

### Network Modes

**Restricted mode (default)** - Blocks local networks, allows internet:
```bash
coi shell  # Default behavior
```
- Blocks: RFC1918 private networks (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
- Blocks: Cloud metadata endpoints (169.254.0.0/16)
- Allows: All public internet (npm, pypi, GitHub, APIs, etc.)

**Allowlist mode** - Only specific domains allowed:
```bash
coi shell --network=allowlist
```
- Requires configuration with `allowed_domains` list
- DNS resolution with automatic IP refresh every 30 minutes
- Always blocks RFC1918 private networks
- IP caching for DNS failure resilience

**Open mode** - No restrictions (trusted projects only):
```bash
coi shell --network=open
```

### Configuration

```toml
# ~/.config/coi/config.toml
[network]
mode = "restricted"  # restricted | open | allowlist

# Allowlist mode configuration
# Supports both domain names and raw IPv4 addresses
allowed_domains = [
    "8.8.8.8",             # Google DNS (REQUIRED for DNS resolution)
    "1.1.1.1",             # Cloudflare DNS (REQUIRED for DNS resolution)
    "registry.npmjs.org",  # npm package registry
    "api.anthropic.com",   # Claude API
    "platform.claude.com", # Claude Platform
]
refresh_interval_minutes = 30  # IP refresh interval (0 to disable)
```

**Important for allowlist mode:**
- **Gateway IP is auto-detected** - COI automatically detects and allows your network gateway IP. You don't need to add it manually. Containers must reach their gateway to route traffic.
- **Public DNS servers required** - `8.8.8.8` and `1.1.1.1` must be in the allowlist for DNS resolution to work.
- **Firewall rule ordering** - COI adds ALLOW rules first (for gateway, allowed domains/IPs), then REJECT rules (for RFC1918 ranges), then a default REJECT rule for allowlist mode.
- Supports both domain names (`github.com`) and raw IPv4 addresses (`8.8.8.8`)
- Subdomains must be listed explicitly (`github.com` ≠ `api.github.com`)
- Domains behind CDNs may have many IPs that change frequently
- DNS failures use cached IPs from previous successful resolution

### Host Access to Container Services

**Accessing services from the host** (e.g., Puma web server, HTTP servers):

By default, COI allows the **host machine** to access services running in containers. This works by adding an allow rule for the gateway IP (which represents the host) **before** the RFC1918 block rules. Since firewalld evaluates rules by priority, the gateway IP is allowed while other private IPs are still blocked.

For example, if a web server runs on port 3000 in the container:
```bash
# Inside container: Puma/Rails server listening on 0.0.0.0:3000
# From host: Access via container IP
curl http://<container-ip>:3000
```

**Allowing access from entire local network:**

For development environments where you want machines on your local network to access container services (e.g., accessing containers via tmux from multiple machines), add this to your config:

```toml
[network]
allow_local_network_access = true  # Allow all RFC1918, not just gateway
```

**⚠️ Security Note:** When `allow_local_network_access = true`, ALL RFC1918 private network traffic is allowed (no RFC1918 blocking). Use this only in trusted development environments where you need cross-machine access.

**Default behavior:** Only the host (gateway IP) can access container services. Other machines on your local network cannot, even if they're on the same subnet.

**Note:** Firewalld rules filter traffic at the FORWARD chain level. All traffic from the container to the gateway IP is permitted to allow host-to-container communication.

### Accessing Container Services from Host

With standard bridge networking, containers are directly accessible from your host. If you run a web server, database, or API inside the container, you can access it from your host browser or tools using the container's IP address.

```bash
# Find container IP
coi list  # Shows IPv4 for running containers

# Access service from host
curl http://<container-ip>:3000
```

**Troubleshooting:**
If you see "Connection refused" when trying to access container services:
1. Verify container service is listening: `coi container exec <name> -- netstat -tlnp`
2. Check container IP: `coi list` (shows IPv4 for running containers)
3. Ensure firewall allows traffic to the bridge network

### Firewalld Setup

Network isolation (restricted/allowlist modes) requires firewalld. If you see the error "firewalld is not available", you have two options:

**Option 1: Use open network mode (quick fix)**
```bash
coi shell --network=open
```
This disables egress filtering but allows you to work immediately.

**Option 2: Install and configure firewalld (recommended)**

Firewalld provides the FORWARD chain filtering needed for network isolation. Follow these steps:

```bash
# 1. Install firewalld (Ubuntu/Debian)
sudo apt install firewalld

# 2. Enable and start firewalld
sudo systemctl enable --now firewalld

# 3. Enable masquerading for container NAT
sudo firewall-cmd --permanent --add-masquerade
sudo firewall-cmd --reload

# 4. Verify firewalld is running
sudo firewall-cmd --state

# 5. Allow COI to manage firewall rules (passwordless sudo)
echo "$USER ALL=(ALL) NOPASSWD: /usr/bin/firewall-cmd" | sudo tee /etc/sudoers.d/coi-firewalld
sudo chmod 440 /etc/sudoers.d/coi-firewalld
```

**Key Points:**
- Firewalld must be running for network isolation to work
- COI adds direct rules to the FORWARD chain to filter container traffic
- Rules are scoped by container IP address for precise filtering
- Rules are removed when containers are stopped/deleted

**How it works:**
- COI gets the container's IP address from Incus
- Firewalld direct rules are added with priorities (lower = evaluated first)
- Restricted mode: Allow gateway, block RFC1918, allow all else
- Allowlist mode: Allow gateway, allow specific IPs, block RFC1918, block all else

## Security Best Practices

### Committing Changes from AI-Generated Code

When working with AI tools in sandboxed containers, be aware that the container has write access to your `.git/` directory through the mounted workspace. This creates a potential attack surface where malicious code could modify git hooks or configuration files.

**The Risk:**
- AI tools can modify `.git/hooks/*` (pre-commit, post-commit, pre-push hooks)
- These hooks execute arbitrary code when you run git commands
- Modified `.gitattributes` can define filters that execute code during git operations
- Git configuration (`.git/config`) could be altered to add malicious aliases

**Best Practice: Disable Hooks When Committing AI-Generated Code**

When committing code that was modified by AI tools, always bypass git hooks:

```bash
# Recommended: Commit with hooks disabled
git -c core.hooksPath=/dev/null commit --no-verify -m "your message"

# Or create an alias for convenience
alias gcs='git -c core.hooksPath=/dev/null commit --no-verify'
```

**Why This Works:**
- `core.hooksPath=/dev/null` tells git to look for hooks in a non-existent directory
- `--no-verify` disables pre-commit, commit-msg, and applypatch-msg hooks
- This prevents any malicious hooks from executing even if they were modified inside the container

**Additional Protection:**

```bash
# Also disable git attributes filters (clean/smudge filters can execute code)
git -c core.hooksPath=/dev/null -c core.attributesFile=/dev/null commit --no-verify -m "msg"

# Or make it a shell function for repeated use
safe_commit() {
    git -c core.hooksPath=/dev/null -c core.attributesFile=/dev/null commit --no-verify "$@"
}
```

**When Is This Necessary?**

This protection is most important when:
- Committing code that was modified or generated by AI tools
- You commit changes **outside** the container (recommended practice)
- Your repository uses git hooks for automation (pre-commit, husky, etc.)

**Note:** COI sandboxes already protect your host environment from malicious code execution. This guidance is specifically about preventing hooks from running when you commit AI-generated changes from your host shell.

## System Health Check

Use `coi health` to diagnose setup issues and verify your environment is correctly configured:

```bash
# Basic health check
coi health

# JSON output for scripting/automation
coi health --format json

# Verbose output with additional checks
coi health --verbose
```

**Example output:**
```
Code on Incus Health Check
==========================

SYSTEM:
  [OK]   Operating system   Ubuntu 24.04.3 LTS (amd64)

CRITICAL:
  [OK]   Incus              Running (version 6.20)
  [OK]   Permissions        User in incus-admin group
  [OK]   Default image      coi (fingerprint: 1bf24b3a67...)
  [OK]   Image age          2 days old

NETWORKING:
  [OK]   Network bridge     incusbr0 (10.128.178.1/24)
  [OK]   IP forwarding      Enabled
  [OK]   Firewalld          Running (restricted mode available)

STORAGE:
  [OK]   COI directory      ~/.coi (writable)
  [OK]   Sessions dir       ~/.coi/sessions-claude (writable)
  [OK]   Disk space         455.0 GB available

CONFIGURATION:
  [OK]   Config loaded      ~/.config/coi/config.toml
  [OK]   Network mode       restricted
  [OK]   Tool               claude

STATUS:
  [OK]   Containers         1 running
  [OK]   Saved sessions     12 session(s)

STATUS: HEALTHY
All 16 checks passed
```

**Exit codes:**
- `0` = healthy (all checks pass)
- `1` = degraded (warnings but functional)
- `2` = unhealthy (critical failures)

**What's checked:**
| Category | Checks |
|----------|--------|
| **System** | OS info, Colima/Lima detection |
| **Critical** | Incus availability, group permissions, default image, image age |
| **Networking** | Network bridge, IP forwarding, firewalld (mode-aware) |
| **Storage** | COI directory, sessions directory, disk space (warns if <5GB) |
| **Configuration** | Config files, network mode, tool |
| **Status** | Running containers, saved sessions |
| **Optional** | DNS resolution, passwordless sudo (with `--verbose`) |

**Colima/Lima detection:** When running inside a Colima or Lima VM, the health check automatically detects this and shows `[colima]` in the OS info. If firewalld is not available, it provides Colima-specific guidance.

## Troubleshooting

### DNS Issues During Build

**Symptom:** `coi build` hangs at "Still waiting for network..." even though the container has an IP address.

**Cause:** On Ubuntu systems with systemd-resolved, containers may receive `127.0.0.53` as their DNS server via DHCP. This is the host's stub resolver which only works on the host, not inside containers.

**Automatic Fix:** COI automatically detects and fixes this issue during build by:
1. Detecting if DNS resolution fails but IP connectivity works
2. Injecting public DNS servers (8.8.8.8, 8.8.4.4, 1.1.1.1) into the container
3. The resulting image uses static DNS configuration

**Permanent Fix:** Configure your Incus network to provide proper DNS to containers:

```bash
# Option 1: Enable managed DNS (recommended)
incus network set incusbr0 dns.mode managed

# Option 2: Use public DNS servers
incus network set incusbr0 raw.dnsmasq "dhcp-option=6,8.8.8.8,8.8.4.4"
```

After applying either fix, future containers will have working DNS automatically.

**Note:** The automatic fix only affects the built image. Other Incus containers on your system may still experience DNS issues until you apply the permanent fix.

**Why doesn't COI automatically run `incus network set` for me?**

COI deliberately uses an in-container fix rather than modifying your Incus network configuration:

1. **System-level impact** - Changing Incus network settings affects all containers on that bridge, not just COI containers
2. **Network name varies** - The bridge might not be named `incusbr0` on all systems
3. **Permissions** - Users running `coi build` might not have permission to modify Incus network settings
4. **Intentional configurations** - Some users have custom DNS configurations for their other containers
5. **Principle of least surprise** - Modifying system-level Incus config without explicit consent could break other setups

The in-container approach is self-contained and only affects COI images, leaving your Incus configuration untouched.
## Frequently Asked Questions

### How is COI different from Docker Sandboxes?

Docker Sandboxes is a Docker Desktop feature that uses microVMs for isolation on macOS/Windows. On Linux, it falls back to traditional containers. COI is built specifically for Linux using Incus system containers:
- **No Docker Desktop needed** - COI uses Incus (fully open source), while Docker Sandboxes requires Docker Desktop (not open source, commercial licensing for organizations)
- **System containers, not microVMs** - One clean isolation layer vs. containers-in-VMs complexity
- **Linux-first design** - Built for Linux from day one, not as an afterthought

### How is COI different from DevContainers?

**Purpose:** DevContainers are for setting up development environments. COI is for securely running AI coding tools that need broad system access.

**Security model:**
- **DevContainers** - Your code runs in the container, but typically with your host credentials mounted
- **COI** - AI tools run in isolated containers **without** your credentials. Only your workspace is mounted, nothing else unless explicitly configured

**Architecture:**
- **DevContainers** - Application containers (Docker) without init systems
- **COI** - System containers (Incus) with full systemd, can run Docker inside

### Can I run COI on Windows?

Not directly. Incus is Linux-only. However, you can:

1. **WSL2 (Windows Subsystem for Linux)** - Install a Linux distribution in WSL2, then install Incus and COI inside WSL2
2. **VM** - Run a Linux VM (Ubuntu, Debian, etc.) and install COI there

Note: Windows support via WSL2 is experimental and not officially tested. Linux or macOS (via Colima/Lima) are the recommended platforms.

### Does COI prevent prompt injection attacks?

**No**, COI does not prevent prompt injection. What COI **does** protect against:

- ✅ **Credential exposure** - Your SSH keys, environment variables, and API tokens are not accessible to AI tools
- ✅ **Host system access** - AI tools can't access your entire filesystem, only the mounted workspace
- ✅ **Lateral movement** - Network isolation prevents access to local network resources (in restricted mode)
- ✅ **Remote code execution blast radius** - Built-in firewalld networking limits access to private networks and metadata services in restricted mode, and allowlist mode can constrain egress to approved domains/IPs, reducing (but not eliminating) data exfiltration and command-and-control risk
- ✅ **Persistent damage** - Ephemeral containers mean any malicious modifications are discarded

What COI **doesn't** protect against:

- ❌ **Prompt injection** - Malicious prompts can still trick the AI into generating harmful code
- ❌ **API key leakage via AI** - If you give the AI your API key, it could be prompted to send it elsewhere
- ❌ **Insecure code generation** - AI-generated code might have vulnerabilities (SQL injection, XSS, etc.)

**Best practices:**

- Review AI-generated code before committing
- Don't mount sensitive credentials into containers
- Use network isolation (restricted/allowlist modes) to limit data exfiltration
- Commit AI changes with git hooks disabled (see [Security Best Practices](#security-best-practices))

### What about API key security?

**If the API key is for the AI tool itself** (e.g., Anthropic API key for Claude):
- Store it in your host `~/.claude/settings.json` or similar config
- COI can mount this file read-only if you enable `mount_claude_config = true` in config
- The AI tool uses the key to authenticate, but it's not available to arbitrary commands in the container

**If you're giving API keys to the AI for it to use** (e.g., AWS keys for the AI to deploy things):
- **Don't do this unless you fully trust the project and AI's capabilities**
- COI isolation prevents credential leakage to your host, but a compromised AI could still misuse those credentials
- Use temporary/scoped credentials with minimal permissions
- Prefer explicit mounting of credentials rather than storing them in the workspace

### Is this really simpler than just running Claude Code directly?

**First-time setup:** `coi build` (one time, 5-10 minutes), then `coi shell` - that's it!

**After setup:** Just `cd your-project && coi shell` - same simplicity as running Claude Code directly, but with:
- ✅ Automatic file ownership (no permission issues)
- ✅ Credential isolation (your SSH keys safe)
- ✅ Session save/resume (continue later)
- ✅ Parallel sessions (multiple workspaces/slots)
- ✅ Clean environment (no host pollution)

The complexity is hidden. You get security and isolation with the same simple workflow.

### What is Incus? (Is it the same as tmux?)

**No, Incus is not tmux.** They're completely different tools:

**Incus** - Linux container and VM manager (like Docker, but for system containers)
- Manages containers with full operating systems inside
- Provides isolation, networking, storage, etc.
- COI uses Incus to create isolated environments

**tmux** - Terminal multiplexer for managing shell sessions
- Lets you detach/reattach terminal sessions
- COI uses tmux *inside* containers to manage AI tool sessions

**In COI:** Incus creates the container, tmux runs inside it to manage your session with the AI tool.

### Why should I trust this?

Fair question! Here's what makes COI trustworthy:

**Open Source** - Full source code at github.com/mensfeld/code-on-incus (MIT license)
- Review the code yourself
- Community can audit and contribute
- No hidden behavior

**Transparent architecture:**
- Uses standard Linux tools (Incus, tmux, systemd)
- No custom daemons or proprietary components
- Easy to inspect running containers (`incus list`, `incus exec`)

**Security by design:**
- Credentials isolated by default (not mounted unless you configure it)
- Network isolation with firewalld (blocks private networks by default)
- Workspace-only mounting (AI can't access your entire filesystem)

**Active development** - Regular updates, responsive to issues, community-driven improvements

### Do I need to give COI "full access"?

**COI itself doesn't need "full access" to anything.** Here's what actually happens:

**COI requires:**
- Incus permissions (you must be in `incus-admin` group)
- Access to your workspace directory (the project you're working on)
- Optional: firewalld sudo for network isolation (you can use `--network=open` to avoid this)

**What AI tools can access:**
- ✅ **Your workspace only** - The project directory you explicitly mount
- ✅ **Container filesystem** - Temporary files that get deleted (ephemeral mode)
- ❌ **Your SSH keys** - Not accessible unless you explicitly mount `~/.ssh`
- ❌ **Your home directory** - Not accessible
- ❌ **Your environment variables** - Not passed to the container
- ❌ **Your local network** - Blocked by default (restricted mode)

You control what gets mounted. By default, COI is locked down.

### Quick Comparison: COI vs. Other Tools

| Tool | Purpose | Credentials Isolated | AI Tool Support | Session Management | Best For |
|------|---------|---------------------|-----------------|-------------------|----------|
| **COI** | AI coding isolation | ✅ Yes (default) | Built-in | Auto save/resume | Running AI coding tools securely |
| **Docker Sandboxes** | AI tool isolation | ✅ Yes | Limited | Manual | macOS/Windows users (requires Docker Desktop) |
| **DevContainers** | Dev environment | ❌ No (typically mounted) | Manual | Manual | Reproducible development environments |
| **Distrobox** | Desktop apps/dev | ❌ No (shares home) | Manual | Manual | Running apps from different distros |
| **Bare metal** | Direct execution | ❌ No (full access) | Manual | None | Maximum performance, trusted environments |

**Choose COI if:** You want AI to modify code without credential exposure, automatic session management, and strong isolation.

