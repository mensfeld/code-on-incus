#!/bin/bash
# Build script for coi image
# This script runs INSIDE the container during image build
#
# It installs all dependencies needed for CLI tool execution:
# - Base development tools
# - Node.js LTS
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
        ripgrep fzf \
        build-essential libssl-dev libreadline-dev zlib1g-dev \
        libffi-dev libyaml-dev libgmp-dev \
        libsqlite3-dev libpq-dev libmysqlclient-dev \
        libxml2-dev libxslt1-dev libcurl4-openssl-dev

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
    chown -R "$CODE_USER:$CODE_USER" "/home/$CODE_USER"

    # Setup passwordless sudo for all commands
    echo "$CODE_USER ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/$CODE_USER"

    chown root:root "/etc/sudoers.d/$CODE_USER"
    chmod 440 "/etc/sudoers.d/$CODE_USER"
    usermod -aG sudo "$CODE_USER"

    log "User '$CODE_USER' created with passwordless sudo (uid: $CODE_UID)"
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

    # Run the native installer as the code user
    # This installs to ~/.local/bin/claude for that user
    su - "$CODE_USER" -c 'curl -fsSL https://claude.ai/install.sh | bash'

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

    su - "$CODE_USER" -c 'curl -fsSL https://raw.githubusercontent.com/opencode-ai/opencode/refs/heads/main/install | bash'

    local OPENCODE_PATH="/home/$CODE_USER/.opencode/bin/opencode"
    if [[ ! -x "$OPENCODE_PATH" ]]; then
        log "ERROR: opencode binary not found at $OPENCODE_PATH after installation."
        log "Installation may have failed or installed to an unexpected location."
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
    configure_power_wrappers
    configure_tmp_cleanup
    install_claude_cli
    install_opencode
    install_dummy
    install_docker
    install_github_cli
    cleanup

    log "coi image build complete!"
}

main "$@"
