# claude-on-incus (`coi`)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mensfeld/claude-on-incus)](https://golang.org/)
[![Latest Release](https://img.shields.io/github/v/release/mensfeld/claude-on-incus)](https://github.com/mensfeld/claude-on-incus/releases)

**The Professional Claude Code Container Runtime for Linux**

Run Claude Code in isolated, production-grade Incus containers with zero permission headaches, perfect file ownership, and true multi-session support.

**Security First:** Unlike Docker or bare-metal execution, your environment variables, SSH keys, and Git credentials are **never** exposed to Claude. Containers run in complete isolation with no access to your host credentials unless explicitly mounted.

*Think Docker for Claude, but with system containers that actually work like real machines.*

## Demo

<!-- Placeholder for asciicast demo - to be added -->

## Features

**Core Capabilities**
- Multi-slot support - Run parallel Claude sessions for the same workspace with full isolation
- Session resume - Resume conversations with full history and credentials restored (workspace-scoped)
- Persistent containers - Keep containers alive between sessions (installed tools preserved)
- Workspace isolation - Each session mounts your project directory
- **Slot isolation** - Each parallel slot has its own home directory (files don't leak between slots)
- **Workspace files persist even in ephemeral mode** - Only the container is deleted, your work is always saved

**Security & Isolation**
- Automatic UID mapping - No permission hell, files owned correctly
- System containers - Full security isolation, better than Docker privileged mode
- Project separation - Complete isolation between workspaces
- **Credential protection** - No risk of SSH keys, `.env` files, or Git credentials being exposed to Claude

**Developer Experience**
- 15+ CLI commands - shell, run, build, list, info, attach, images, clean, kill, shutdown, tmux, version, container, file, image
- Shell completions - Built-in bash/zsh/fish completions via `coi completion`
- Smart configuration - TOML-based with profiles and hierarchy
- Tmux integration - Background processes and session management
- Claude config mounting - Automatic `~/.claude` and `.claude.json` sync (enabled by default)

**Safe `--dangerous` Flags**
- Claude Code CLI uses `--dangerously-disable-sandbox` and `--dangerously-allow-write-to-root` flags
- **These are safe inside containers** because the "root" is the container root, not your host system
- Containers are ephemeral or isolated - any changes are contained and don't affect your host
- This gives Claude full capabilities while keeping your system protected

## Quick Start

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/mensfeld/claude-on-incus/master/install.sh | bash

# Build image (first time only, ~5-10 minutes)
coi build

# Start coding
cd your-project
coi shell

# That's it! Claude is now running in an isolated container with:
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

| Feature | **claude-on-incus (Incus)** | Docker |
|---------|---------------------------|--------|
| **Container Type** | System containers (full OS) | Application containers |
| **Init System** | Full systemd/init | No init (single process) |
| **UID Mapping** | Automatic UID shifting | Manual mapping required |
| **Security** | Unprivileged by default | Often requires privileged mode |
| **File Permissions** | Preserved (UID shifting) | Host UID conflicts |
| **Startup Time** | ~1-2 seconds | ~0.5-1 second |
| **Docker-in-Container** | Native support | Requires DinD hacks |

### Benefits

**No Permission Hell** - Incus automatically maps container UIDs to host UIDs. Files created by Claude in-container have correct ownership on host. No `chown` needed.

**True Isolation** - Full system container means Claude can run Docker, systemd services, etc. Safer than Docker's privileged mode.

**Persistent State** - System containers can be stopped/started without data loss. Ideal for long-running Claude sessions.

**Resource Efficiency** - Share kernel like Docker, lower overhead than VMs, better density for parallel sessions.

## Installation

```bash
# One-shot install (recommended)
curl -fsSL https://raw.githubusercontent.com/mensfeld/claude-on-incus/master/install.sh | bash

# This will:
# - Download and install coi to /usr/local/bin
# - Check for Incus installation
# - Verify you're in incus-admin group
# - Show next steps
```

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
- Claude CLI
- GitHub CLI (`gh`)
- tmux for session management
- Common build tools

**Custom images:** Build your own specialized images using build scripts that run on top of the base `coi` image.

### Verify Installation

```bash
coi version        # Check version
incus version      # Verify Incus access
groups | grep incus-admin  # Confirm group membership
```

## Usage

### Basic Commands

```bash
# Interactive Claude session
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

Session resume allows you to continue a previous Claude conversation with full history and credentials restored.

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
- Claude credentials (no re-authentication needed)
- User settings and preferences
- Project context and conversation state

**How It Works:**
- After each session, `.claude` directory is automatically saved to `~/.coi/sessions/`
- On resume, session data is restored to the container before Claude starts
- Fresh credentials are injected from your host `~/.claude` directory
- Claude automatically continues from where you left off

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

[paths]
sessions_dir = "~/.coi/sessions"
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

## Use Cases

| Use Case | Problem | Solution |
|----------|---------|----------|
| **Individual Developers** | Multiple projects with different tool versions | Each project gets isolated container with specific tools |
| **Teams** | "Works on my machine" syndrome | Share `.coi.toml`, everyone gets identical environment |
| **AI/ML Development** | Need Docker inside container | Incus natively supports Docker-in-container, no DinD hacks |
| **Security-Conscious** | Can't use Docker privileged mode | True isolation without privileged mode |

## Requirements

- **Incus** - Linux container manager
- **Go 1.21+** - For building from source
- **incus-admin group** - User must be in group

## Performance: Fast Storage

By default, Incus uses directory-based storage which copies entire filesystems when creating containers. For **instant container creation**, use ZFS or Btrfs which support copy-on-write cloning.

### Setting Up ZFS Storage (Recommended)

```bash
# Install ZFS
sudo apt install zfsutils-linux

# Create a ZFS storage pool (50GB loopback file)
sudo incus storage create zfs-pool zfs size=50GiB

# Or use a dedicated partition for best performance
# sudo incus storage create zfs-pool zfs source=/dev/nvme0n1p4

# Update the default profile to use ZFS
incus profile device set default root pool=zfs-pool
```

### Performance Comparison

| Storage Type | Container Creation | Copy Mechanism |
|--------------|-------------------|----------------|
| **dir** (default) | ~10-30 seconds | Full filesystem copy |
| **zfs** | < 1 second | Copy-on-write clone |
| **btrfs** | < 1 second | Copy-on-write clone |

After switching to ZFS, new containers use instant snapshots. Existing containers remain on the old storage pool.

## Troubleshooting

### "incus is not available"
```bash
sudo apt update && sudo apt install -y incus
sudo incus admin init --auto
sudo usermod -aG incus-admin $USER
# Log out and back in
```

### "permission denied" errors
```bash
groups | grep incus-admin  # Check membership
sudo usermod -aG incus-admin $USER  # Add yourself
# Log out and back in
```

### Container won't start
```bash
incus info  # Check daemon status
sudo systemctl start incus
```

## Project Status

**Production Ready** - All core features are fully implemented and tested.

**Implemented Features:**
- Core commands: shell, run, build, list, info, attach, images, clean, kill, shutdown, tmux, version
- Advanced operations: container (launch/start/stop/delete/exec/mount), file (push/pull), image (list/publish/delete/cleanup)
- Multi-slot parallel sessions
- Session resume with full conversation history and credentials restoration
- Persistent containers with state preservation
- Custom image building from user scripts
- Low-level container and file transfer operations
- Automatic UID mapping
- TOML-based configuration with profiles
- Comprehensive integration test suite (54 tests passing)

## Container Lifecycle & Session Persistence

Understanding how containers and sessions work in `coi`:

### How It Works Internally

1. **Containers are always launched as non-ephemeral** (persistent in Incus terms)
   - This allows saving session data even if the container is stopped from within (e.g., `sudo shutdown 0`)
   - Session data can be pulled from stopped containers, but not from deleted ones

2. **Inside the container**: `tmux` → `bash` → `claude`
   - When claude exits, you're dropped to bash
   - From bash you can: type `exit`, press `Ctrl+b d` to detach, or run `sudo shutdown 0`

3. **On cleanup** (when you exit/detach):
   - Session data (`.claude` directory) is **always** saved to `~/.coi/sessions/`
   - If `--persistent` was NOT set: container is deleted after saving
   - If `--persistent` was set: container is kept for reuse

### What Gets Preserved

| Mode | Workspace Files | Claude Session | Container State |
|------|----------------|----------------|-----------------|
| **Default (ephemeral)** | Always saved | Always saved | Deleted |
| **`--persistent`** | Always saved | Always saved | Kept |

### Session vs Container Persistence

- **`--resume`**: Restores the **Claude conversation** in a fresh container
  - Use when you want to continue a conversation but don't need installed packages
  - Container is recreated, only `.claude` session data is restored
  - **Workspace-scoped**: Only finds sessions from the current workspace directory (security feature)

- **`--persistent`**: Keeps the **entire container** with all modifications
  - Use when you've installed tools, built artifacts, or modified the environment
  - `coi attach` reconnects to the same container with everything intact

### Stopping Containers

From **inside** the container:
- `exit` in bash → saves session, then deletes container (or keeps if `--persistent`)
- `Ctrl+b d` → detaches, saves session, container stays running
- `sudo shutdown 0` → stops container, session is saved, then container is deleted (or kept if `--persistent`)

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
coi shell                    # Start session
# ... work with claude ...
exit                         # Exit bash → session saved, container deleted
coi shell --resume           # Continue conversation in fresh container
```

**Long-running project (`--persistent`):**
```bash
coi shell --persistent       # Start persistent session
# ... install tools, build things ...
# Press Ctrl+b d to detach
coi attach                   # Reconnect to same container with all tools
coi shutdown --all           # When done, clean up
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
# - Claude conversation history

# List both running sessions
coi list
#   coi-abc12345-1 (ephemeral)
#   coi-abc12345-2 (ephemeral)
```

## License

MIT

## Author

Maciej Mensfeld ([@mensfeld](https://github.com/mensfeld))

## See Also

- [FAQ](FAQ.md) - Frequently asked questions
- [CHANGELOG](CHANGELOG.md) - Version history and release notes
- [Integration Tests](INTE.md) - Comprehensive E2E testing documentation
- [Incus](https://linuxcontainers.org/incus/) - Linux container manager
