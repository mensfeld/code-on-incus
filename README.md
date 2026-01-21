<p align="center">
  <img src="misc/logo.png" alt="Code on Incus Logo" width="350">
</p>

# code-on-incus (`coi`)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mensfeld/code-on-incus)](https://golang.org/)
[![Latest Release](https://img.shields.io/github/v/release/mensfeld/code-on-incus)](https://github.com/mensfeld/code-on-incus/releases)

**Secure and Fast Container Runtime for AI Coding Tools on Linux**

Run AI coding assistants (Claude Code, Aider, and more) in isolated, production-grade Incus containers with zero permission headaches, perfect file ownership, and true multi-session support.

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


## Why Incus Over Docker?

### What is Incus?

Incus is a modern Linux container and virtual machine manager, forked from LXD. Unlike Docker (which uses application containers), Incus provides **system containers** that behave like lightweight VMs with full init systems.

### Key Differences

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

**No Permission Hell** - Incus automatically maps container UIDs to host UIDs. Files created by AI tools in-container have correct ownership on host. No `chown` needed.

**True Isolation** - Full system container means AI tools can run Docker, systemd services, etc. Safer than Docker's privileged mode.

**Persistent State** - System containers can be stopped/started without data loss. Ideal for long-running AI coding sessions.

**Resource Efficiency** - Share kernel like Docker, lower overhead than VMs, better density for parallel sessions.

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
allowed_domains = ["github.com", "api.anthropic.com", "8.8.8.8"]
refresh_interval_minutes = 30  # IP refresh interval (0 to disable)
```

**Important for allowlist mode:**
- Supports both domain names (`github.com`) and raw IPv4 addresses (`8.8.8.8`)
- Subdomains must be listed explicitly (`github.com` ≠ `api.github.com`)
- Domains behind CDNs may have many IPs that change frequently
- DNS failures use cached IPs from previous successful resolution
- To allow DNS resolution inside the container, add DNS server IPs (e.g., `8.8.8.8`)

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
