# claude-on-incus (`coi`)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mensfeld/claude-on-incus)](https://golang.org/)
[![Latest Release](https://img.shields.io/github/v/release/mensfeld/claude-on-incus)](https://github.com/mensfeld/claude-on-incus/releases)

**The Professional Claude Code Container Runtime for Linux**

Run Claude Code in isolated, production-grade Incus containers with zero permission headaches, perfect file ownership, and true multi-session support.

*Think Docker for Claude, but with system containers that actually work like real machines.*

## Features

### 🚀 Core Capabilities
- ✅ **Multi-slot support** - Run parallel Claude sessions for the same workspace
- ✅ **Session persistence** - Resume sessions with `.claude` directory restoration
- ✅ **Persistent containers** - Keep containers alive between sessions (installed tools preserved)
- ✅ **Workspace isolation** - Each session mounts your project directory

### 🔒 Security & Isolation
- ✅ **Automatic UID mapping** - No permission hell, files owned correctly
- ✅ **System containers** - Full security isolation, better than Docker privileged mode
- ✅ **Project separation** - Complete isolation between workspaces

### 🛠️ Developer Experience
- ✅ **10 CLI commands** - shell, run, build, list, info, attach, images, clean, tmux, version
- ✅ **Shell completions** - Built-in bash/zsh/fish completions via `coi completion`
- ✅ **Smart configuration** - TOML-based with profiles and hierarchy
- ✅ **Tmux integration** - Background processes and session management
- ✅ **Claude config mounting** - Automatic `~/.claude` sync (enabled by default)

## 30-Second Demo

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/mensfeld/claude-on-incus/master/install.sh | bash

# Setup (first time only, ~5-10 minutes)
coi build sandbox

# Start coding
cd your-project
coi shell

# That's it! Claude is now running in an isolated container with:
# ✓ Your project mounted at /workspace
# ✓ Correct file permissions (no more chown!)
# ✓ Full Docker access inside the container
# ✓ All changes persisted automatically
```

## Why Incus Over Docker?

### What is Incus?

Incus is a modern Linux container and virtual machine manager, forked from LXD. Unlike Docker (which uses application containers), Incus provides **system containers** that behave like lightweight VMs with full init systems.

### Key Differences

| Feature | **claude-on-incus (Incus)** | Docker (e.g., claudebox) |
|---------|---------------------------|--------------------------|
| **Container Type** | System containers (full OS) | Application containers |
| **Init System** | ✅ Full systemd/init | ❌ No init (single process) |
| **UID Mapping** | ✅ Automatic UID shifting | ⚠️ Manual mapping required |
| **Security** | ✅ Unprivileged by default | ⚠️ Often requires privileged mode |
| **File Permissions** | ✅ Preserved (UID shifting) | ❌ Host UID conflicts |
| **Resource Limits** | ✅ Granular cgroup control | ✅ Cgroup limits |
| **Startup Time** | ~1-2 seconds | ~0.5-1 second |
| **Multi-user Support** | ✅ Full user namespace | ⚠️ Limited |
| **Docker-in-Container** | ✅ Native support | ⚠️ Requires DinD hacks |

### Benefits of Incus for Claude Code

1. **No Permission Hell**
   - Incus automatically maps container UIDs to host UIDs
   - Files created by Claude in-container have correct ownership on host
   - No `chown` needed after container operations

2. **True Isolation**
   - Full system container = Claude can run Docker, systemd services, etc.
   - Safer than Docker's privileged mode
   - Better multi-tenant security

3. **Persistent State**
   - System containers can be stopped/started without data loss
   - Ideal for long-running Claude sessions
   - Snapshots and versioning built-in

4. **Better for Development Workflows**
   - Claude can install system packages (`apt install`)
   - Full init system for complex toolchains
   - Native Docker support (no DinD)

5. **Resource Efficiency**
   - Share kernel like Docker
   - Lower overhead than VMs
   - Better density for parallel sessions

### When to Use Docker Instead

- **Docker is better if:**
  - You only need short-lived, single-command runs
  - You're on macOS/Windows (Incus is Linux-only)
  - You prefer simpler, more familiar tooling
  - You don't need full system access

- **Incus is better if:**
  - You need persistent, long-running sessions
  - You want to run Docker inside the container
  - You care about file permission correctness
  - You need better security isolation
  - You're running on Linux

### Example: The Permission Problem

**Docker:**
```bash
# In container (as root/user 1000)
docker run -v $PWD:/workspace my-image touch /workspace/file.txt

# On host
ls -la file.txt
# -rw-r--r-- 1 root root  # Wrong! Need chown
```

**Incus:**
```bash
# In container (as claude/UID 1000)
incus exec claude -- touch /workspace/file.txt

# On host
ls -la file.txt
# -rw-r--r-- 1 youruser youruser  # Correct! Auto-mapped
```

### Architecture Comparison

**Docker (Application Container):**
```
┌─────────────────────────────┐
│ Claude Process              │
│ (PID 1, no init)            │
└─────────────────────────────┘
│ Host Kernel                 │
```

**Incus (System Container):**
```
┌─────────────────────────────┐
│ Init System (systemd)       │
│   ├─ Claude Process         │
│   ├─ Docker Daemon          │
│   └─ Other Services         │
└─────────────────────────────┘
│ Host Kernel                 │
```

### Similar Projects

- **[claudebox](https://github.com/RchGrav/claudebox)** - Docker-based (great for macOS/Windows)
- **[run-claude-docker](https://github.com/icanhasjonas/run-claude-docker)** - Minimal Docker approach
- **claude-on-incus** - Linux system containers for power users

## Quick Start

### Step 1: Install

```bash
curl -fsSL https://raw.githubusercontent.com/mensfeld/claude-on-incus/master/install.sh | bash
```

This will:
- ✓ Download and install `coi` to `/usr/local/bin`
- ✓ Check for Incus installation
- ✓ Verify you're in `incus-admin` group
- ✓ Show next steps

### Step 2: Build Images (First Time Only)

```bash
# Basic image (5-10 minutes)
coi build sandbox

# Optional: Privileged image with Git/SSH (adds 2-3 minutes)
coi build privileged
```

**What's in the images?**
- `coi-sandbox`: Ubuntu 22.04 + Docker + Node.js 20 + Claude CLI + tmux
- `coi-privileged`: Everything above + GitHub CLI + SSH + Git config

### Step 3: Start Your First Session

```bash
cd your-project
coi shell
```

**That's it!** You're now in an isolated container with:
- Your project mounted at `/workspace`
- Full Docker access
- Correct file permissions
- Claude CLI ready to use

### Step 4: Learn More

```bash
coi --help          # See all commands
coi shell --help    # Shell command options
coi list            # List active sessions
```

## Installation

### From Source

```bash
git clone https://github.com/mensfeld/claude-on-incus
cd claude-on-incus
make install
```

### One-Shot Install

```bash
# Install latest version
curl -fsSL https://raw.githubusercontent.com/mensfeld/claude-on-incus/master/install.sh | bash

# Install specific version
VERSION=v0.1.0 curl -fsSL https://raw.githubusercontent.com/mensfeld/claude-on-incus/master/install.sh | bash

# Build from source instead
curl -fsSL https://raw.githubusercontent.com/mensfeld/claude-on-incus/master/install.sh | bash -s -- --source
```

### Verify Installation

After installation, verify everything works:

```bash
# Check version
coi version
# Expected: claude-on-incus (coi) v0.1.0

# Verify Incus access
incus version
# Should show version without errors

# Check group membership
groups | grep incus-admin
# Should show: incus-admin

# Test basic command
coi --help
# Should show help text
```

**If any command fails:**
- Not in `incus-admin` group? → Log out and back in
- `incus` not found? → Install Incus (see [Requirements](#requirements))
- Permission errors? → Run `sudo usermod -aG incus-admin $USER`

## What's Next?

### Learn the Basics

```bash
# Start a session
coi shell

# List all sessions
coi list

# Resume a previous session
coi shell --resume

# Attach to running session
coi attach
```

### Enable Persistent Mode

```bash
# Keep container between sessions
coi shell --persistent

# Install tools once, use forever
> sudo apt install ripgrep fd-find bat
> cargo install exa
> # Exit and restart - tools still there!
```

### Work on Multiple Projects

```bash
# Each project gets its own container
cd ~/project-a
coi shell --slot 1 &

cd ~/project-b
coi shell --slot 2 &

# Containers are isolated, files are separate
```

### Advanced Usage

- 📚 Read the [full documentation](#usage)
- 🔧 Configure [profiles](#configuration)
- 🧪 See [integration tests](INTE.md) for workflow examples

## Use Cases

### 👨‍💻 Individual Developers

**Problem:** Working on multiple projects with different tool versions

**Solution:** Each project gets its own container with specific tools

```bash
# Project A needs Node 18
cd ~/project-a
coi shell --persistent
> nvm install 18

# Project B needs Node 20
cd ~/project-b
coi shell --persistent
> nvm install 20

# Tools stay isolated, no conflicts!
```

### 👥 Teams

**Problem:** "Works on my machine" syndrome

**Solution:** Share configuration files, everyone gets identical environment

```bash
# Commit .claude-on-incus.toml to your repo
# Team members just:
cd your-project
coi shell

# Everyone has the same environment!
```

### 🔬 AI/ML Development

**Problem:** Need Docker inside container for model training

**Solution:** Incus natively supports Docker-in-container

```bash
coi shell --privileged --persistent
> docker run --gpus all nvidia/cuda:12.0-base
> # Full Docker access, no DinD hacks!
```

### 🏢 Security-Conscious Environments

**Problem:** Can't use Docker privileged mode

**Solution:** Incus provides security without sacrificing functionality

```bash
# True isolation, no privileged mode needed
coi shell --persistent
> # Full system container, but isolated
```

## Usage

### Basic Commands

```bash
# Run a command in ephemeral container
coi run "npm test"

# Start interactive Claude session
coi shell

# Persistent mode - keep container between sessions
coi shell --persistent

# Use specific slot for parallel sessions
coi shell --slot 2

# Privileged mode (Git/SSH access)
coi shell --privileged

# Persistent + privileged (install tools once, use forever)
coi shell --persistent --privileged

# Resume previous session
coi shell --resume

# Attach to existing session
coi attach                    # List sessions or auto-attach if only one
coi attach claude-abc123-1    # Attach to specific session

# Build images
coi build sandbox
coi build privileged

# List available images
coi images                    # Show COI images
coi images --all              # Show all local images

# List active sessions
coi list

# Show session info
coi info                      # Most recent session
coi info <session-id>         # Specific session

# Tmux integration
coi tmux send <container> "command"   # Send command to tmux
coi tmux capture <container>          # Capture tmux output
coi tmux list <container>             # List tmux sessions

# Cleanup
coi clean

# Version info
coi version
```

### Global Flags

- `--workspace PATH` - Workspace directory to mount (default: current directory)
- `--slot NUMBER` - Slot number for parallel sessions (0 = auto-allocate)
- `--privileged` - Use privileged image (Git/SSH/sudo)
- `--persistent` - Keep container between sessions (preserves installed packages, build artifacts)
- `--resume [SESSION_ID]` - Resume from session
- `--profile NAME` - Use named profile
- `--env KEY=VALUE` - Set environment variables
- `--storage PATH` - Mount persistent storage

## Configuration

Config file: `~/.config/claude-on-incus/config.toml`

```toml
[defaults]
image = "coi-sandbox"
privileged = false
persistent = true  # Set to true to keep containers between sessions
mount_claude_config = true

[paths]
sessions_dir = "~/.claude-on-incus/sessions"
storage_dir = "~/.claude-on-incus/storage"

[incus]
project = "default"
group = "incus-admin"
claude_uid = 1000

[profiles.rust]
image = "coi-rust"
environment = { RUST_BACKTRACE = "1" }
persistent = true  # Keep Rust tools installed
```

### Configuration Hierarchy

Settings are loaded in order (highest precedence last):
1. Built-in defaults
2. System config (`/etc/claude-on-incus/config.toml`)
3. User config (`~/.config/claude-on-incus/config.toml`)
4. Project config (`./.claude-on-incus.toml`)
5. Environment variables (`CLAUDE_ON_INCUS_*`)
6. CLI flags (`--persistent`, etc.)

**See the configuration section below for detailed persistent mode configuration.**

## Persistent Mode

By default, containers are **ephemeral** (deleted on exit). Enable **persistent mode** to keep containers between sessions:

### Benefits
- ✅ **Install once, use forever** - `apt install`, `npm install`, etc. persist
- ✅ **Faster startup** - Reuse existing container instead of rebuilding
- ✅ **Build artifacts preserved** - No re-compiling on each session
- ✅ **Development-friendly** - Matches real development workflows

### Quick Enable

**Via CLI flag:**
```bash
coi shell --persistent
```

**Via config (recommended):**
```toml
# ~/.config/claude-on-incus/config.toml
[defaults]
persistent = true
```

**Via environment variable:**
```bash
export CLAUDE_ON_INCUS_PERSISTENT=true
coi shell
```

### Example Workflow

```bash
# First session - install tools
coi shell --persistent
> sudo apt-get install -y jq ripgrep fd-find
> npm install
> exit

# Second session - tools already there!
coi shell --persistent
> which jq     # ✅ /usr/bin/jq (no reinstall needed)
> npm test     # ✅ node_modules already present
```

## Architecture

```
┌─────────────────────────────────────────┐
│  CLI (Cobra)                            │
│  shell | run | build | clean | list     │
└────────────────┬────────────────────────┘
                 │
┌────────────────┴────────────────────────┐
│  Session Orchestrator                   │
│  • Setup → Run → Cleanup                │
│  • .claude save/restore                 │
└────────────────┬────────────────────────┘
                 │
┌────────────────┴────────────────────────┐
│  Container Manager                      │
│  • Launch/stop/delete                   │
│  • Mount management                     │
│  • File push/pull                       │
└────────────────┬────────────────────────┘
                 │
┌────────────────┴────────────────────────┐
│  Incus Commands                         │
│  • sg + incus CLI wrappers              │
└─────────────────────────────────────────┘
```

## Project Status

**Production Ready** - All core features are fully implemented and tested.

### Implemented Features ✅

**CLI Commands:**
- ✅ `shell` - Interactive Claude sessions with full resume support
- ✅ `run` - Execute commands in ephemeral containers
- ✅ `build` - Build sandbox and privileged Incus images
- ✅ `list` - List active containers and saved sessions
- ✅ `info` - Show detailed session information
- ✅ `attach` - Attach to running Claude sessions
- ✅ `images` - List available Incus images
- ✅ `clean` - Clean up stopped containers and old sessions
- ✅ `tmux` - Tmux integration for background processes
- ✅ `version` - Show version information

**Session Management:**
- ✅ Multi-slot parallel sessions (run multiple Claude instances)
- ✅ Session persistence with `.claude` state restoration
- ✅ Persistent containers (keep installed packages between sessions)
- ✅ Automatic session saving and cleanup
- ✅ Resume from previous sessions with full state
- ✅ Graceful Ctrl+C handling

**Container & Workspace:**
- ✅ Sandbox image (`coi-sandbox`: Ubuntu 22.04 + Docker + Node.js + Claude CLI + tmux)
- ✅ Privileged image (`coi-privileged`: + GitHub CLI + SSH + Git config)
- ✅ Automatic UID mapping (correct file permissions)
- ✅ Workspace isolation and mounting
- ✅ Environment variable passing
- ✅ Persistent storage mounting
- ✅ Claude config mounting (automatic ~/.claude sync)

**Configuration:**
- ✅ TOML-based configuration system
- ✅ Profile support with environment overrides
- ✅ Configuration hierarchy (system → user → project → env → flags)

**Testing:**
- ✅ Comprehensive integration test suite (3,900+ lines)
- ✅ CLI command tests
- ✅ Feature scenario tests
- ✅ Error handling tests

### Future Enhancements
- [x] One-shot installer script
- [ ] Release binaries (GitHub releases) - *Note: installer script ready*
- [ ] Profile checksum validation
- [ ] Developer ergonomics (zsh + delta + fzf)
- [ ] JSON output mode for programmatic use
- [ ] Container health checks and auto-recovery
- [ ] Shell completions (bash, zsh, fish)

## Requirements

- **Incus** - Linux container manager
- **Go 1.21+** - For building from source
- **incus-admin group** - User must be in incus-admin group

## Troubleshooting

### Common Issues

#### "incus is not available"
```bash
# Install Incus (Ubuntu/Debian)
sudo apt update && sudo apt install -y incus

# Initialize Incus
sudo incus admin init --auto

# Add yourself to the group
sudo usermod -aG incus-admin $USER
# Log out and back in for changes to take effect
```

#### "permission denied" errors
Make sure you're in the `incus-admin` group:
```bash
groups | grep incus-admin
```

If not, add yourself and restart your session:
```bash
sudo usermod -aG incus-admin $USER
# Log out and log back in
```

#### Container won't start
Check if Incus daemon is running:
```bash
incus info
```

If not running:
```bash
sudo systemctl start incus
sudo systemctl enable incus
```

#### Files created in container have wrong owner
This should never happen with Incus! If it does:
1. Verify you're using `coi-sandbox` or `coi-privileged` images
2. Check UID mapping: `incus config get <container> raw.idmap`
3. Report as a bug - this is a core feature!

### Getting Help

- 📖 [Full Documentation](https://github.com/mensfeld/claude-on-incus)
- 🐛 [Report Issues](https://github.com/mensfeld/claude-on-incus/issues)
- 💬 [Discussions](https://github.com/mensfeld/claude-on-incus/discussions)

## FAQ

### How is this different from Docker?

See the ["Why Incus Over Docker?"](#why-incus-over-docker) section above. TL;DR:
- **Better file permissions** - No more `chown` after every operation
- **True isolation** - System containers, not application containers
- **Native Docker support** - Run Docker inside without DinD hacks
- **Multi-user friendly** - Proper UID namespacing

### Can I run this on macOS or Windows?

**No.** Incus is Linux-only because it uses Linux kernel features (namespaces, cgroups).

For macOS/Windows, use:
- [claudebox](https://github.com/RchGrav/claudebox) (Docker-based)
- [run-claude-docker](https://github.com/icanhasjonas/run-claude-docker)

### Can I run multiple Claude sessions on the same project?

**Yes!** Use slots:

```bash
# Terminal 1
coi shell --slot 1

# Terminal 2 (same project)
coi shell --slot 2

# Terminal 3 (same project)
coi shell --slot 3
```

Each slot gets its own container but shares the workspace files.

### How much disk space do I need?

- **Incus itself:** ~100MB
- **coi-sandbox image:** ~800MB
- **coi-privileged image:** ~1GB
- **Per container (persistent):** ~200MB base + your tools

Recommendation: **5GB free space** for comfortable usage.

### Is this production-ready?

**Yes!** All core features are implemented and tested:
- ✅ 3,900+ lines of integration tests
- ✅ Comprehensive error handling
- ✅ Stable API

Current version: **0.1.0** (see [CHANGELOG](CHANGELOG.md))

### How do I update?

```bash
# Re-run installer
curl -fsSL https://raw.githubusercontent.com/mensfeld/claude-on-incus/master/install.sh | bash

# Or build from source
cd claude-on-incus
git pull
make install
```

Containers and sessions are preserved during updates.

## License

MIT

## Author

Maciej Mensfeld ([@mensfeld](https://github.com/mensfeld))

## See Also

- [CHANGELOG](CHANGELOG.md) - Version history and release notes
- [Integration Tests](INTE.md) - Comprehensive E2E testing documentation (215 test cases)
- [claudebox](https://github.com/RchGrav/claudebox) - Docker-based alternative (macOS/Windows)
- [run-claude-docker](https://github.com/icanhasjonas/run-claude-docker) - Minimal Docker approach
- [Incus](https://linuxcontainers.org/incus/) - Linux container manager
