#!/bin/bash
# Build script for coi image
# This script runs INSIDE the container during image build
#
# It installs all dependencies needed for CLI tool execution:
# - Base development tools
# - Node.js LTS (system, for Claude CLI)
# - mise (polyglot runtime manager) with Python 3, pnpm, npm:typescript, npm:tsx
# - Claude CLI
# - Docker
# - GitHub CLI
# - dummy (test stub for testing)

set -euo pipefail

# Configuration
CODE_USER="code"
CODE_UID=1000

log() {
    echo "[coi] $*"
}

#######################################
# Configure DNS if misconfigured
# Only applies fix if DNS resolution fails
#######################################
configure_dns_if_needed() {
    log "Checking DNS configuration..."

    # Test if DNS resolution works
    if getent hosts archive.ubuntu.com > /dev/null 2>&1; then
        log "DNS resolution works, keeping default configuration."
        return 0
    fi

    log "DNS resolution failed, configuring static DNS..."

    # Disable systemd-resolved (not needed in containers)
    systemctl disable systemd-resolved 2>/dev/null || true
    systemctl stop systemd-resolved 2>/dev/null || true
    systemctl mask systemd-resolved 2>/dev/null || true

    # Remove symlink and create static resolv.conf
    rm -f /etc/resolv.conf
    cat > /etc/resolv.conf << 'EOF'
# Static DNS configuration (auto-configured due to DNS misconfiguration)
# See: https://github.com/mensfeld/code-on-incus#troubleshooting
nameserver 8.8.8.8
nameserver 8.8.4.4
nameserver 1.1.1.1
EOF

    log "Static DNS configured (8.8.8.8, 8.8.4.4, 1.1.1.1)."

    # Verify it works now
    if getent hosts archive.ubuntu.com > /dev/null 2>&1; then
        log "DNS resolution now working."
    else
        log "WARNING: DNS still not working after fix. Build may fail."
    fi
}

#######################################
# Install base dependencies
#######################################
install_base_dependencies() {
    log "Installing base dependencies..."

    apt-get update -qq

    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
        curl wget git ca-certificates gnupg jq unzip sudo \
        tmux \
        dnsutils \
        ripgrep fzf fd-find bat tree \
        strace lsof \
        sqlite3 postgresql-client redis-tools \
        imagemagick \
        build-essential libssl-dev libreadline-dev zlib1g-dev \
        libffi-dev libyaml-dev libgmp-dev \
        libsqlite3-dev libpq-dev libmysqlclient-dev \
        libxml2-dev libxslt1-dev libcurl4-openssl-dev

    # On Ubuntu/Debian, fd-find and bat install as fdfind and batcat.
    # Create symlinks so the standard names work.
    ln -sf "$(command -v fdfind)" /usr/local/bin/fd
    ln -sf "$(command -v batcat)" /usr/local/bin/bat

    log "Base dependencies installed"
}

#######################################
# Install Node.js LTS
#######################################
install_nodejs() {
    log "Installing Node.js LTS..."

    curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
    apt-get install -y -qq nodejs

    log "Node.js $(node --version) installed"
}

#######################################
# Configure mise and install default runtimes
#
# mise manages Python, Node.js tools (pnpm, tsx), and other runtimes.
# System Node.js (from nodesource) is kept for Claude CLI and core
# tooling; mise handles everything else.
#
# See: https://mise.jdx.dev
#######################################
configure_mise_tools() {
    log "Configuring mise shell activation and default tools..."

    local CODE_HOME="/home/$CODE_USER"

    # Add mise activation to .bashrc so all interactive and login shells
    # (including those started by claude/opencode) pick up mise-managed tools.
    local BASHRC="$CODE_HOME/.bashrc"
    if ! grep -q 'mise activate' "$BASHRC" 2>/dev/null; then
        cat >> "$BASHRC" << 'MISE_EOF'

# mise: polyglot runtime manager
# Adds mise-managed tools (python, pnpm, tsx, etc.) to PATH
eval "$(mise activate bash)"
MISE_EOF
    fi

    # Also add to .profile for non-interactive login shells (e.g. `su - code -c "..."`)
    local PROFILE="$CODE_HOME/.profile"
    if ! grep -q 'mise activate' "$PROFILE" 2>/dev/null; then
        cat >> "$PROFILE" << 'MISE_EOF'

# mise: polyglot runtime manager
eval "$(mise activate bash)"
MISE_EOF
    fi

    # Add a system-wide profile hook so shells that source /etc/profile
    # (including non-interactive shells started by COI tooling via bash -c
    # inside tmux) get mise-managed shims on PATH as well.
    if [ ! -f /etc/profile.d/mise.sh ]; then
        cat > /etc/profile.d/mise.sh << 'MISE_PROFILE_EOF'
# system-wide mise activation for all users
if command -v mise >/dev/null 2>&1; then
    eval "$(mise activate bash)"
fi
MISE_PROFILE_EOF
        chmod 644 /etc/profile.d/mise.sh
    fi

    # Install default tools globally via mise as the code user.
    # This sets ~/.config/mise/config.toml with global tool versions.
    local attempt
    for attempt in 1 2 3; do
        if su - "$CODE_USER" -c 'mise use --global python@3 pnpm@latest npm:typescript@latest npm:tsx@latest'; then
            break
        fi
        if [ "$attempt" -eq 3 ]; then
            log "ERROR: mise tool installation failed after 3 attempts."
            exit 1
        fi
        log "mise install failed (attempt $attempt/3), retrying in 10s..."
        sleep 10
    done

    # Verify tools are installed and accessible via mise
    for bin in python3 pnpm tsc tsx; do
        if ! su - "$CODE_USER" -c "mise exec -- which $bin" >/dev/null 2>&1; then
            log "ERROR: '$bin' not found via mise after installation."
            exit 1
        fi
    done

    # Ensure the code user owns all mise state
    chown -R "$CODE_USER:$CODE_USER" "$CODE_HOME/.local/share/mise" 2>/dev/null || true
    chown -R "$CODE_USER:$CODE_USER" "$CODE_HOME/.config/mise" 2>/dev/null || true

    log "mise tools installed (python3, pnpm, typescript, tsx)"
}

#######################################
# Create code user with passwordless sudo
#######################################
create_code_user() {
    log "Creating code user..."

    # Rename ubuntu user to code
    usermod -l "$CODE_USER" -d "/home/$CODE_USER" -m ubuntu
    groupmod -n "$CODE_USER" ubuntu
    mkdir -p "/home/$CODE_USER/.claude"
    mkdir -p "/home/$CODE_USER/.ssh"
    chmod 700 "/home/$CODE_USER/.ssh"
    ssh-keyscan -t ed25519,rsa,ecdsa github.com gitlab.com bitbucket.org >> "/home/$CODE_USER/.ssh/known_hosts" 2>/dev/null
    chmod 644 "/home/$CODE_USER/.ssh/known_hosts"
    chown -R "$CODE_USER:$CODE_USER" "/home/$CODE_USER"

    # Setup passwordless sudo for all commands
    echo "$CODE_USER ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/$CODE_USER"

    chown root:root "/etc/sudoers.d/$CODE_USER"
    chmod 440 "/etc/sudoers.d/$CODE_USER"
    usermod -aG sudo "$CODE_USER"

    log "User '$CODE_USER' created with passwordless sudo (uid: $CODE_UID)"
}

#######################################
# Install mise (polyglot runtime manager)
# See: https://mise.jdx.dev
#######################################
install_mise() {
    log "Installing mise..."

    # Install to /usr/local/bin so it's available system-wide
    local attempt
    for attempt in 1 2 3; do
        if curl -fsSL https://mise.jdx.dev/install.sh | MISE_INSTALL_PATH=/usr/local/bin/mise sh; then
            break
        fi
        if [ "$attempt" -eq 3 ]; then
            log "ERROR: mise installation failed after 3 attempts."
            exit 1
        fi
        log "mise install failed (attempt $attempt/3), retrying in 10s..."
        sleep 10
    done

    if [[ ! -x /usr/local/bin/mise ]]; then
        log "ERROR: mise binary not found at /usr/local/bin/mise after installation."
        exit 1
    fi

    log "mise $(/usr/local/bin/mise --version 2>/dev/null || echo 'installed')"
}

#######################################
# Configure power management wrappers
#######################################
configure_power_wrappers() {
    log "Configuring power management command wrappers..."

    # Create wrapper scripts that use sudo automatically
    # This allows users to type "poweroff" instead of "sudo poweroff"
    # while working around the lack of login sessions in containers

    for cmd in shutdown poweroff reboot halt; do
        cat > "/usr/local/bin/${cmd}" << 'WRAPPER_EOF'
#!/bin/bash
# Wrapper to run power management commands with sudo automatically
# This works around the lack of login sessions in container environments
exec sudo /usr/sbin/COMMAND_NAME "$@"
WRAPPER_EOF
        # Replace COMMAND_NAME with actual command
        sed -i "s/COMMAND_NAME/${cmd}/" "/usr/local/bin/${cmd}"
        chmod 755 "/usr/local/bin/${cmd}"
    done

    log "Power management wrappers configured"
}

#######################################
# Install Claude CLI using native installer
# Note: npm installation is deprecated as of 2025
# See: https://code.claude.com/docs/en/setup
#######################################
install_claude_cli() {
    log "Installing Claude CLI (native)..."

    # Prefer IPv4 to work around broken IPv6 in containers and some networks.
    # The native installer (Bun/Node) resolves AAAA records first; when the
    # IPv6 path is non-functional the download either times out or returns 403.
    # See: https://github.com/anthropics/claude-code/issues/13498
    if ! grep -q '::ffff:0:0/96' /etc/gai.conf 2>/dev/null; then
        echo 'precedence ::ffff:0:0/96 100' >> /etc/gai.conf
        log "IPv4 preference set in /etc/gai.conf"
    fi

    # Run the native installer as the code user (with retries for transient network failures)
    local attempt
    for attempt in 1 2 3; do
        if su - "$CODE_USER" -c 'curl -4 -fsSL https://claude.ai/install.sh | bash'; then
            break
        fi
        if [ "$attempt" -eq 3 ]; then
            log "ERROR: Claude CLI installation failed after 3 attempts."
            exit 1
        fi
        log "Claude CLI install failed (attempt $attempt/3), retrying in 10s..."
        sleep 10
    done

    # Verify that the installer actually created the Claude CLI binary
    local CLAUDE_PATH="/home/$CODE_USER/.local/bin/claude"
    if [[ ! -x "$CLAUDE_PATH" ]]; then
        log "ERROR: Claude CLI binary not found at $CLAUDE_PATH after installation."
        log "Installation may have failed or installed to an unexpected location."
        exit 1
    fi

    # Create a global symlink so it's accessible system-wide
    ln -sf "$CLAUDE_PATH" /usr/local/bin/claude

    log "Claude CLI $(claude --version 2>/dev/null || echo 'installed')"
}

#######################################
# Install opencode
# See: https://opencode.ai
#######################################
install_opencode() {
    log "Installing opencode..."

    # Download the binary directly from the /latest/download/ redirect URL.
    # This does NOT hit the GitHub API and is not subject to rate limits
    # (unlike the official installer which calls api.github.com).
    local INSTALL_DIR="/home/$CODE_USER/.opencode/bin"
    local OPENCODE_PATH="$INSTALL_DIR/opencode"
    local DOWNLOAD_URL="https://github.com/anomalyco/opencode/releases/latest/download/opencode-linux-x64.tar.gz"

    mkdir -p "$INSTALL_DIR"
    chown "$CODE_USER:$CODE_USER" "$INSTALL_DIR"

    local attempt
    for attempt in 1 2 3; do
        if curl -fsSL "$DOWNLOAD_URL" | tar xz -C "$INSTALL_DIR"; then
            chmod +x "$OPENCODE_PATH"
            chown "$CODE_USER:$CODE_USER" "$OPENCODE_PATH"
            break
        fi
        if [ "$attempt" -eq 3 ]; then
            log "ERROR: opencode installation failed after 3 attempts."
            exit 1
        fi
        log "opencode download failed (attempt $attempt/3), retrying in 10s..."
        sleep 10
    done

    if [[ ! -x "$OPENCODE_PATH" ]]; then
        log "ERROR: opencode binary not found at $OPENCODE_PATH after installation."
        exit 1
    fi

    ln -sf "$OPENCODE_PATH" /usr/local/bin/opencode

    log "opencode $(opencode --version 2>/dev/null || echo 'installed')"
}

#######################################
# Install dummy (test stub for testing)
#######################################
install_dummy() {
    log "Installing dummy (test stub for testing)..."

    # dummy MUST be pushed to /tmp/dummy before running this script
    if [[ ! -f /tmp/dummy ]]; then
        log "ERROR: /tmp/dummy not found!"
        log "The dummy script must be pushed to the container before building."
        log "Make sure you're running the build from the project root directory."
        exit 1
    fi

    cp /tmp/dummy /usr/local/bin/dummy
    chmod +x /usr/local/bin/dummy
    rm /tmp/dummy
    log "dummy $(dummy --version 2>/dev/null || echo 'installed')"
}

#######################################
# Install Docker CE
#######################################
install_docker() {
    log "Installing Docker..."

    # Add Docker GPG key
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg

    # Add Docker repository
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null

    # Install Docker
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
        docker-ce docker-ce-cli containerd.io \
        docker-buildx-plugin docker-compose-plugin

    # Add code user to docker group (belt-and-suspenders, may not be sufficient
    # on its own — see daemon config below for the reliable fix)
    usermod -aG docker "$CODE_USER"

    # Make the Docker socket accessible to the code user's PRIMARY group.
    #
    # Why: incus exec may not call initgroups() when --group is specified,
    # so supplementary groups (including 'docker') may not be active in the
    # session. The user's primary group (code, GID 1000) is always active
    # regardless of how the session was started.
    #
    # Two layers of config are needed:
    #
    # 1. daemon.json "group": "code" — Docker daemon chowns the socket to
    #    root:code on startup (works when Docker creates the socket itself).
    #
    # 2. docker.socket systemd drop-in SocketGroup=code — Ubuntu's Docker
    #    package uses systemd socket activation: systemd creates
    #    /var/run/docker.sock before the daemon starts using the group from
    #    the socket unit (default: docker). The daemon.json setting alone is
    #    not enough when systemd socket activation is in play; this drop-in
    #    ensures the socket is created with root:code (0660) from the start.
    mkdir -p /etc/docker
    cat > /etc/docker/daemon.json << 'EOF'
{
    "group": "code"
}
EOF

    mkdir -p /etc/systemd/system/docker.socket.d
    cat > /etc/systemd/system/docker.socket.d/override.conf << 'EOF'
[Socket]
SocketGroup=code
EOF

    log "Docker $(docker --version 2>/dev/null || echo 'installed')"
}

#######################################
# Install GitHub CLI
#######################################
install_github_cli() {
    log "Installing GitHub CLI..."

    # Add GitHub CLI GPG key
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg

    # Add GitHub CLI repository
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null

    # Install
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq gh

    log "GitHub CLI $(gh --version 2>/dev/null | head -1 || echo 'installed')"
}

#######################################
# Configure /tmp auto-cleanup
#
# By default Ubuntu's systemd-tmpfiles-clean.timer runs daily and only
# removes files older than 10 days. AI coding agents can fill /tmp in
# minutes, so we:
#   1. Lower the age threshold to 1 hour so abandoned temp files are
#      collected promptly.
#   2. Run the cleanup timer every 15 minutes instead of daily so recovery
#      happens automatically between heavy operations.
#
# This complements the hard tmpfs size cap set by COI at container start.
#######################################
configure_tmp_cleanup() {
    log "Configuring /tmp auto-cleanup..."

    # Age threshold: remove files in /tmp not accessed for more than 1 hour.
    # The 'D' type removes the directory contents but keeps /tmp itself.
    cat > /etc/tmpfiles.d/coi-tmp-cleanup.conf << 'EOF'
# COI: clean files in /tmp that have not been accessed for 1 hour.
# This prevents abandoned build artefacts from exhausting the tmpfs.
D /tmp 1777 root root 1h
EOF

    # Override the cleanup timer to run every 15 minutes.
    mkdir -p /etc/systemd/system/systemd-tmpfiles-clean.timer.d
    cat > /etc/systemd/system/systemd-tmpfiles-clean.timer.d/coi-interval.conf << 'EOF'
[Timer]
# Reset inherited values before setting our own
OnBootSec=
OnUnitActiveSec=
# Start 5 minutes after boot, then every 15 minutes
OnBootSec=5min
OnUnitActiveSec=15min
EOF

    # Enable the timer (it is masked in some minimal Ubuntu images)
    systemctl enable systemd-tmpfiles-clean.timer 2>/dev/null || true

    log "/tmp cleanup configured (1h age threshold, 15min timer)"
}

#######################################
# Configure tmux scrollback history
#
# `coi shell` runs the interactive session inside a tmux session so
# that an exit of the inner CLI (e.g. Ctrl-C out of opencode) does not
# tear down the shell. Tmux's default `history-limit` is 2000 lines,
# which silently truncates the beginning of long command outputs
# (e.g. `bin/setup` in a Rails app) so the user cannot scroll back to
# the start. Bump the default high enough to cover realistic build
# logs. Users who want a different value can override this by writing
# their own `~/.tmux.conf` — tmux loads it after `/etc/tmux.conf` so
# per-user settings always win.
#######################################
configure_tmux() {
    log "Configuring tmux scrollback history..."

    cat > /etc/tmux.conf << 'EOF'
# COI default: large scrollback so long build outputs (bin/setup, npm ci,
# cargo build, etc.) are fully retrievable. Override in ~/.tmux.conf —
# tmux loads the per-user file after this one, so your value wins.
set -g history-limit 50000
EOF
    chmod 644 /etc/tmux.conf

    log "tmux history-limit set to 50000 in /etc/tmux.conf"
}

#######################################
# Cleanup
#######################################
cleanup() {
    log "Cleaning up..."
    apt-get clean
    rm -rf /var/lib/apt/lists/*
    log "Cleanup complete"
}

#######################################
# Main
#######################################
main() {
    log "Starting coi image build..."

    configure_dns_if_needed
    install_base_dependencies
    install_nodejs
    create_code_user
    install_mise
    configure_mise_tools
    configure_power_wrappers
    configure_tmp_cleanup
    configure_tmux
    install_claude_cli
    install_opencode
    install_dummy
    install_docker
    install_github_cli
    cleanup

    log "coi image build complete!"
}

main "$@"
