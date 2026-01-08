# claude-on-incus (`coi`)

Run Claude Code in isolated Incus containers with session persistence, workspace isolation, and multi-slot support.

## Features

- ✅ **Multi-slot support** - Run parallel Claude sessions for the same workspace
- ✅ **Session persistence** - Resume sessions with `.claude` directory restoration
- ✅ **Persistent containers** - Keep containers alive between sessions (installed tools preserved)
- ✅ **Workspace isolation** - Each session mounts your project directory
- ✅ **Session management** - Auto-save/restore session data on every run
- ✅ **Interactive shell** - `coi shell` command with full resume support
- ✅ **Image building** - Sandbox and privileged images with Docker + build tools
- ✅ **Configuration system** - TOML-based config with profiles
- ✅ **Management commands** - List, info, attach, images, clean, and tmux commands

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

```bash
# Build the binary
make build

# Build COI images
coi build sandbox     # Standard sandbox image (coi-sandbox)
coi build privileged  # Privileged image with Git/SSH (coi-privileged)

# Run a command
coi run "echo hello"

# Start interactive session
coi shell

# Run parallel sessions
coi shell --slot 1  # First session
coi shell --slot 2  # Second session
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
# Coming soon
curl -sSL https://get.claude-on-incus.dev | bash
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

**See [PERSISTENT_MODE.md](PERSISTENT_MODE.md) for detailed persistent mode configuration.**

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

**See [PERSISTENT_MODE.md](PERSISTENT_MODE.md) for complete guide.**

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
- ✅ `tmux` - Tmux integration for ClaudeYard
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
- [ ] One-shot installer script
- [ ] Release binaries (GitHub releases)
- [ ] Profile checksum validation
- [ ] Developer ergonomics (zsh + delta + fzf)
- [ ] JSON output mode for programmatic use

## Requirements

- **Incus** - Linux container manager
- **Go 1.21+** - For building from source
- **incus-admin group** - User must be in incus-admin group

## License

MIT

## Author

Maciej Mensfeld ([@mensfeld](https://github.com/mensfeld))

## See Also

- [ClaudeYard](https://github.com/mensfeld/claude_yard) - Workflow automation using claude-on-incus
- [claudebox](https://github.com/RchGrav/claudebox) - Docker-based alternative
- [Incus](https://linuxcontainers.org/incus/) - Linux container manager
