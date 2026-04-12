#!/usr/bin/env bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REPO="mensfeld/code-on-incus"
BINARY_NAME="coi"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

# Detect non-interactive mode (CI, explicit opt-in, or no usable controlling terminal)
if [ "${NONINTERACTIVE:-0}" = "1" ] || [ "${CI:-}" = "true" ] || ! { true </dev/tty; } 2>/dev/null; then
    NONINTERACTIVE=1
else
    NONINTERACTIVE=0
fi

# Prompt user for yes/no confirmation.
# In non-interactive mode (curl|bash, CI), exits with error since we can't ask.
# In interactive mode, reads from /dev/tty so it works even when script is piped.
prompt_continue() {
    local message="${1:-Continue anyway?}"
    if [ "$NONINTERACTIVE" = "1" ]; then
        echo -e "${YELLOW}⚠ Non-interactive mode: cannot prompt. Aborting.${NC}"
        echo "  Re-run the script directly (not piped) or fix the issue above."
        exit 1
    fi
    read -p "$message [y/N] " -n 1 -r </dev/tty
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
}

# Prompt user for a choice (single character).
# In non-interactive mode, returns the default value.
# In interactive mode, reads from /dev/tty.
prompt_choice() {
    local message="$1"
    local default="$2"
    if [ "$NONINTERACTIVE" = "1" ]; then
        echo -e "${BLUE}→ Non-interactive mode: using default ($default)${NC}"
        REPLY="$default"
        return
    fi
    read -p "$message" -n 1 -r </dev/tty
    echo ""
    if [ -z "$REPLY" ]; then
        REPLY="$default"
    fi
}

# Detect OS and architecture
detect_platform() {
    local os
    local arch

    os="$(uname -s)"
    arch="$(uname -m)"

    case "$os" in
        Linux*)
            OS="linux"
            ;;
        *)
            echo -e "${RED}✗ Unsupported OS: $os${NC}"
            echo "  code-on-incus requires Linux (Incus is Linux-only)"
            echo "  On macOS: Run inside a Colima or Lima VM"
            echo "  See: https://github.com/mensfeld/code-on-incus#running-on-macos-colimalima"
            exit 1
            ;;
    esac

    case "$arch" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            echo -e "${RED}✗ Unsupported architecture: $arch${NC}"
            exit 1
            ;;
    esac

    echo -e "${BLUE}→ Detected platform: ${OS}/${ARCH}${NC}"
}

# Check if Incus is installed
check_incus() {
    echo -e "${BLUE}→ Checking Incus installation...${NC}"

    if ! command -v incus &> /dev/null; then
        echo -e "${YELLOW}⚠ Incus not found${NC}"
        echo ""
        echo "  claude-on-incus requires Incus to be installed."
        echo "  Install Incus: https://linuxcontainers.org/incus/docs/main/installing/"
        echo ""
        echo "  Quick install (Ubuntu/Debian):"
        echo "    sudo apt update"
        echo "    sudo apt install -y incus"
        echo "    sudo incus admin init --auto"
        echo "    sudo usermod -aG incus-admin \$USER"
        echo ""
        prompt_continue "Continue installation anyway?"
    else
        local incus_version_output
        incus_version_output="$(incus version 2>/dev/null)"
        echo -e "${GREEN}✓ Incus found: ${incus_version_output}${NC}"

        # Check minimum version (>= 6.1)
        local server_version
        server_version="$(echo "$incus_version_output" | grep '^Server version:' | cut -d: -f2 | tr -d ' ')"
        if [ -z "$server_version" ]; then
            # Fallback: single-line output (older Incus)
            server_version="$(echo "$incus_version_output" | head -n1 | tr -d ' ')"
        fi

        if [ -n "$server_version" ]; then
            local ver_major ver_minor
            ver_major="$(echo "$server_version" | cut -d. -f1)"
            ver_minor="$(echo "$server_version" | cut -d. -f2)"

            if [ -n "$ver_major" ] && [ -n "$ver_minor" ]; then
                if [ "$ver_major" -lt 6 ] || { [ "$ver_major" -eq 6 ] && [ "$ver_minor" -lt 1 ]; }; then
                    echo ""
                    echo -e "${YELLOW}⚠ Incus version ${server_version} is below the minimum required 6.1${NC}"
                    echo ""
                    echo "  Ubuntu ships Incus 6.0.x which lacks required idmapping support."
                    echo "  You may see errors like:"
                    echo "    'Failed to setup device mount: idmapping abilities are required'"
                    echo ""
                    echo "  Please install Incus >= 6.1 from the Zabbly repository:"
                    echo "    https://github.com/zabbly/incus"
                    echo ""
                    prompt_continue "Continue installation anyway?"
                fi
            fi
        fi
    fi
}

# Check if user is in incus-admin group
check_group() {
    if groups | grep -q incus-admin; then
        echo -e "${GREEN}✓ User is in incus-admin group${NC}"
    else
        echo -e "${YELLOW}⚠ User is not in incus-admin group${NC}"
        echo ""
        echo "  You need to be in the incus-admin group to use claude-on-incus."
        echo "  Run: sudo usermod -aG incus-admin \$USER"
        echo "  Then log out and back in for changes to take effect."
        echo ""
    fi
}

# Ensure the Incus bridge is in the firewalld trusted zone
# Without this, containers may fail to get IP addresses
ensure_bridge_trusted_zone() {
    # Try to detect the Incus bridge name
    local bridge_name=""
    if command -v incus &> /dev/null; then
        bridge_name=$(incus network list -f csv 2>/dev/null | grep ',bridge,' | head -1 | cut -d, -f1)
    fi
    # Fallback to incusbr0
    if [ -z "$bridge_name" ]; then
        bridge_name="incusbr0"
    fi

    # Check if bridge is already in trusted zone
    if sudo -n firewall-cmd --zone=trusted --query-interface="$bridge_name" &> /dev/null; then
        echo -e "${GREEN}✓ Bridge $bridge_name is in firewalld trusted zone${NC}"
        return
    fi

    echo -e "${YELLOW}⚠ Bridge $bridge_name is not in firewalld trusted zone${NC}"
    echo ""
    echo "  Without this, containers may fail to get IP addresses."
    echo ""

    if [ "$NONINTERACTIVE" = "1" ]; then
        echo -e "${BLUE}→ Non-interactive mode: adding bridge to trusted zone...${NC}"
        sudo firewall-cmd --zone=trusted --add-interface="$bridge_name" --permanent
        sudo firewall-cmd --reload
        echo -e "${GREEN}✓ Bridge $bridge_name added to trusted zone${NC}"
        return
    fi

    read -p "  Add $bridge_name to firewalld trusted zone now? [Y/n] " -n 1 -r </dev/tty
    echo ""
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
        echo -e "${BLUE}→ Adding $bridge_name to trusted zone...${NC}"
        sudo firewall-cmd --zone=trusted --add-interface="$bridge_name" --permanent
        sudo firewall-cmd --reload
        echo -e "${GREEN}✓ Bridge $bridge_name added to trusted zone${NC}"
    fi
}

# Check for ufw conflict before installing firewalld
check_ufw() {
    echo -e "${BLUE}→ Checking for ufw...${NC}"

    if ! command -v ufw &> /dev/null; then
        # ufw not installed, nothing to do
        return
    fi

    # Check if ufw is active (systemctl doesn't require sudo)
    if ! systemctl is-active --quiet ufw 2>/dev/null; then
        echo -e "${GREEN}✓ ufw is installed but inactive (no conflict)${NC}"
        return
    fi

    # ufw is active — this conflicts with firewalld
    echo -e "${YELLOW}⚠ ufw is active${NC}"
    echo ""
    echo "  ufw and firewalld both manage netfilter rules. Running both at the"
    echo "  same time will silently break container networking."
    echo ""
    echo "  Options:"
    echo "    1) Disable ufw and continue with firewalld setup (recommended)"
    echo "    2) Keep ufw and skip firewalld (only --network=open will work)"
    echo ""

    if [ "$NONINTERACTIVE" = "1" ]; then
        echo -e "${RED}✗ Cannot proceed: ufw is active and conflicts with firewalld.${NC}"
        echo "  Disable ufw first: sudo ufw disable && sudo systemctl disable --now ufw"
        echo "  Then re-run this installer."
        exit 1
    fi

    prompt_choice "  Choose [1/2] (default: 1): " "1"

    case "$REPLY" in
        2)
            echo -e "${BLUE}→ Keeping ufw, skipping firewalld setup${NC}"
            SKIP_FIREWALLD=1
            ;;
        *)
            echo -e "${BLUE}→ Disabling ufw...${NC}"
            sudo ufw disable
            sudo systemctl disable --now ufw
            echo -e "${GREEN}✓ ufw disabled${NC}"
            ;;
    esac
}

# Check firewalld for network isolation
check_firewalld() {
    echo -e "${BLUE}→ Checking firewalld (for network isolation)...${NC}"

    if ! command -v firewall-cmd &> /dev/null; then
        echo -e "${YELLOW}⚠ Firewalld not found${NC}"
        echo ""
        echo "  Network isolation (restricted/allowlist modes) requires firewalld."
        echo "  Without it, you can still use --network=open mode."
        echo ""

        if [ "$NONINTERACTIVE" = "1" ]; then
            echo -e "${BLUE}→ Non-interactive mode: skipping firewalld setup (optional)${NC}"
            return
        fi

        read -p "  Install and configure firewalld now? [y/N] " -n 1 -r </dev/tty
        echo ""
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo -e "${BLUE}→ Installing firewalld...${NC}"
            sudo apt install -y firewalld
            echo -e "${BLUE}→ Enabling and starting firewalld...${NC}"
            sudo systemctl enable --now firewalld
            echo -e "${BLUE}→ Enabling masquerade (required for container internet access)...${NC}"
            sudo firewall-cmd --permanent --add-masquerade
            sudo firewall-cmd --reload
            echo -e "${BLUE}→ Setting up passwordless sudo for firewall-cmd...${NC}"
            local fwcmd_path
            fwcmd_path="$(command -v firewall-cmd)"
            echo "$USER ALL=(ALL) NOPASSWD: $fwcmd_path" | sudo tee /etc/sudoers.d/coi-firewalld > /dev/null
            sudo chmod 0440 /etc/sudoers.d/coi-firewalld
            echo -e "${GREEN}✓ Firewalld installed and configured${NC}"
            ensure_bridge_trusted_zone
        fi
        return
    fi

    if ! sudo -n firewall-cmd --state &> /dev/null 2>&1; then
        echo -e "${YELLOW}⚠ Firewalld is installed but not running${NC}"
        echo ""
        echo "  Network isolation (restricted/allowlist modes) requires firewalld."
        echo ""

        if [ "$NONINTERACTIVE" = "1" ]; then
            echo -e "${BLUE}→ Non-interactive mode: skipping firewalld start (optional)${NC}"
            return
        fi

        read -p "  Start and enable firewalld now? [y/N] " -n 1 -r </dev/tty
        echo ""
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo -e "${BLUE}→ Enabling and starting firewalld...${NC}"
            sudo systemctl enable --now firewalld
            echo -e "${GREEN}✓ Firewalld started${NC}"

            # Check masquerade after starting
            if ! sudo -n firewall-cmd --query-masquerade &> /dev/null; then
                echo -e "${BLUE}→ Enabling masquerade (required for container internet access)...${NC}"
                sudo firewall-cmd --permanent --add-masquerade
                sudo firewall-cmd --reload
                echo -e "${GREEN}✓ Masquerade enabled${NC}"
            fi
            ensure_bridge_trusted_zone
        fi
        return
    fi

    # Firewalld is installed and running — check masquerade
    if sudo -n firewall-cmd --query-masquerade &> /dev/null; then
        echo -e "${GREEN}✓ Firewalld is running with masquerade enabled${NC}"
    else
        echo -e "${YELLOW}⚠ Firewalld is running but masquerade is not enabled${NC}"
        echo ""
        echo "  Masquerade is required for containers to reach the internet."
        echo ""

        if [ "$NONINTERACTIVE" = "1" ]; then
            echo -e "${BLUE}→ Non-interactive mode: skipping masquerade setup (optional)${NC}"
            ensure_bridge_trusted_zone
            return
        fi

        read -p "  Enable masquerade now? [y/N] " -n 1 -r </dev/tty
        echo ""
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo -e "${BLUE}→ Enabling masquerade...${NC}"
            sudo firewall-cmd --permanent --add-masquerade
            sudo firewall-cmd --reload
            echo -e "${GREEN}✓ Masquerade enabled${NC}"
        fi
    fi

    # Check bridge trusted zone (regardless of masquerade status)
    ensure_bridge_trusted_zone
}

# Download binary from GitHub releases
download_binary() {
    local download_url
    local tmp_dir
    local binary_path

    echo -e "${BLUE}→ Downloading claude-on-incus...${NC}"

    tmp_dir="$(mktemp -d)"
    trap "rm -rf '$tmp_dir'" EXIT

    if [ "$VERSION" = "latest" ]; then
        download_url="https://github.com/${REPO}/releases/latest/download/coi-${OS}-${ARCH}"
    else
        download_url="https://github.com/${REPO}/releases/download/${VERSION}/coi-${OS}-${ARCH}"
    fi

    binary_path="${tmp_dir}/${BINARY_NAME}"

    if command -v curl &> /dev/null; then
        curl -fsSL "$download_url" -o "$binary_path"
    elif command -v wget &> /dev/null; then
        wget -q -O "$binary_path" "$download_url"
    else
        echo -e "${RED}✗ Neither curl nor wget found${NC}"
        echo "  Please install curl or wget and try again."
        exit 1
    fi

    chmod +x "$binary_path"

    # Install to system
    echo -e "${BLUE}→ Installing to ${INSTALL_DIR}...${NC}"

    if [ -w "$INSTALL_DIR" ]; then
        cp "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
        ln -sf "${INSTALL_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/claude-on-incus"
    else
        sudo cp "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
        sudo ln -sf "${INSTALL_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/claude-on-incus"
    fi

    echo -e "${GREEN}✓ Installed to ${INSTALL_DIR}/${BINARY_NAME}${NC}"

    # Grant immutable-attribute capability for host-side protected path hardening
    grant_immutable_capability
}

# Build from source
build_from_source() {
    local tmp_dir

    echo -e "${BLUE}→ Building from source...${NC}"

    # Check for Go
    if ! command -v go &> /dev/null; then
        echo -e "${RED}✗ Go not found${NC}"
        echo "  Install Go: https://go.dev/doc/install"
        exit 1
    fi

    echo -e "${BLUE}→ Go version: $(go version)${NC}"

    tmp_dir="$(mktemp -d)"
    trap "rm -rf '$tmp_dir'" EXIT

    # Clone repository
    echo -e "${BLUE}→ Cloning repository...${NC}"
    git clone --depth 1 "https://github.com/${REPO}.git" "$tmp_dir"

    # Build (as the current user — never under sudo, because sudo strips PATH
    # and user-scoped Go toolchains like mise/asdf/$HOME/go/bin would disappear).
    cd "$tmp_dir"
    echo -e "${BLUE}→ Building binary...${NC}"
    make build

    # Install the freshly-built binary. We intentionally do NOT run
    # `sudo make install` here: that would re-invoke the `build` prerequisite
    # under sudo, strip PATH, and crash on systems where Go is user-scoped.
    # Mirror download_binary and copy the binary directly, only sudo-ing when
    # $INSTALL_DIR is not writable.
    echo -e "${BLUE}→ Installing to ${INSTALL_DIR}...${NC}"
    local built_binary="${tmp_dir}/${BINARY_NAME}"
    if [ -w "$INSTALL_DIR" ]; then
        cp "$built_binary" "${INSTALL_DIR}/${BINARY_NAME}"
        ln -sf "${INSTALL_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/claude-on-incus"
    else
        sudo cp "$built_binary" "${INSTALL_DIR}/${BINARY_NAME}"
        sudo ln -sf "${INSTALL_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/claude-on-incus"
    fi

    echo -e "${GREEN}✓ Built and installed${NC}"

    # Grant immutable-attribute capability for host-side protected path hardening
    grant_immutable_capability
}

# Grant CAP_LINUX_IMMUTABLE on the installed binary so COI can apply
# chattr +i on host-side protected paths (defense-in-depth against
# unshare+umount bypass of read-only bind mounts).
grant_immutable_capability() {
    if command -v setcap &> /dev/null; then
        if sudo setcap cap_linux_immutable=ep "${INSTALL_DIR}/${BINARY_NAME}" 2>/dev/null; then
            echo -e "${GREEN}✓ Granted cap_linux_immutable capability (host-side path protection)${NC}"
        else
            echo -e "${YELLOW}⚠ Could not set cap_linux_immutable on binary${NC}"
            echo "  Host-side immutable protection will be unavailable."
            echo "  To enable: sudo setcap cap_linux_immutable=ep ${INSTALL_DIR}/${BINARY_NAME}"
        fi
    else
        echo -e "${YELLOW}⚠ setcap not found (install libcap2-bin)${NC}"
        echo "  Host-side immutable protection will be unavailable."
    fi
}

# Ensure Incus has been initialized (creates default network, profile devices, etc.)
ensure_incus_initialized() {
    # Skip if Incus is not installed
    if ! command -v incus &> /dev/null; then
        return
    fi

    # Check if Incus has been initialized by looking for any networks.
    # `incus admin init` creates incusbr0; an empty list means never initialized.
    # If the query itself fails (daemon down, no permissions), warn and bail out
    # rather than incorrectly triggering init.
    local networks
    if ! networks="$(incus network list --format=csv 2>/dev/null)"; then
        echo -e "${YELLOW}⚠ Unable to determine whether Incus has been initialized${NC}"
        echo "  Could not query Incus networks. Ensure the Incus daemon is running and your user has access."
        return 1
    fi
    if [ -n "$networks" ]; then
        return
    fi

    echo -e "${BLUE}→ Incus has not been initialized, running incus admin init --auto...${NC}"
    local output
    if output="$(sudo incus admin init --auto 2>&1)"; then
        echo -e "${GREEN}✓ Incus initialized${NC}"
    else
        echo -e "${YELLOW}⚠ Incus initialization failed${NC}"
        if [ -n "$output" ]; then
            printf "  %s\n" "$output"
        fi
        return 1
    fi
}

# Set up ZFS storage (for instant container creation)
setup_zfs_storage() {
    echo ""
    echo -e "${BLUE}→ Setting up fast storage (ZFS)...${NC}"

    # Check if ZFS is already installed
    if command -v zfs &> /dev/null; then
        echo -e "${GREEN}✓ ZFS already installed${NC}"
    else
        echo -e "${BLUE}→ Installing ZFS...${NC}"
        if sudo apt-get install -y zfsutils-linux 2>&1 | grep -q "E:"; then
            echo -e "${YELLOW}⚠ ZFS installation failed (may not be available for your kernel)${NC}"
            echo -e "${YELLOW}  Containers will use default storage (slower but functional)${NC}"
            return 1
        fi
        echo -e "${GREEN}✓ ZFS installed${NC}"
    fi

    # Check if ZFS pool already exists
    if incus storage list --format=csv 2>/dev/null | grep -q "^zfs-pool,"; then
        echo -e "${GREEN}✓ ZFS storage pool already configured${NC}"
        return 0
    fi

    # Create ZFS storage pool
    echo -e "${BLUE}→ Creating ZFS storage pool (50GiB)...${NC}"
    local storage_output
    if storage_output="$(sudo incus storage create zfs-pool zfs size=50GiB 2>&1)"; then
        echo -e "${GREEN}✓ ZFS storage pool created${NC}"

        # Configure default profile to use ZFS
        echo -e "${BLUE}→ Configuring default profile to use ZFS...${NC}"
        local profile_output
        if profile_output="$(incus profile device set default root pool=zfs-pool 2>&1)"; then
            echo -e "${GREEN}✓ Default profile configured for ZFS${NC}"
            echo -e "${GREEN}✓ Containers will now start instantly (~50ms vs 5-10s)${NC}"
        else
            echo -e "${YELLOW}⚠ Failed to configure default profile${NC}"
            if [ -n "$profile_output" ]; then
                printf "${YELLOW}  %s${NC}\n" "$profile_output"
            fi
            echo -e "${YELLOW}  You can manually configure it later with:${NC}"
            echo -e "  ${BLUE}incus profile device set default root pool=zfs-pool${NC}"
        fi
    else
        echo -e "${YELLOW}⚠ ZFS storage pool creation failed${NC}"
        if [ -n "$storage_output" ]; then
            printf "${YELLOW}  %s${NC}\n" "$storage_output"
        fi
        echo -e "${YELLOW}  Containers will use default storage (slower but functional)${NC}"
        return 1
    fi
}

# Post-install setup
post_install() {
    ensure_incus_initialized || true

    # Try to set up ZFS storage (best-effort, don't abort installer on failure)
    setup_zfs_storage || true

    echo ""
    echo -e "${GREEN}✓ Installation complete!${NC}"
    echo ""
    echo "Next steps:"
    echo ""
    echo "  1. Build the COI image:"
    echo "     ${BLUE}coi build${NC}"
    echo ""
    echo "  2. Start your first session:"
    echo "     ${BLUE}coi shell${NC}"
    echo ""
    echo "  3. View available commands:"
    echo "     ${BLUE}coi --help${NC}"
    echo ""

    if ! groups | grep -q incus-admin; then
        echo -e "${YELLOW}⚠ Remember to add yourself to incus-admin group:${NC}"
        echo "   ${BLUE}sudo usermod -aG incus-admin \$USER${NC}"
        echo "   Then log out and back in."
        echo ""
    fi

    if [ "${SKIP_FIREWALLD:-0}" = "1" ]; then
        echo -e "${YELLOW}⚠ Firewalld was skipped (ufw is active) — only --network=open will work.${NC}"
        echo "   To enable network isolation later, disable ufw and install firewalld:"
        echo "   ${BLUE}sudo ufw disable && sudo systemctl disable --now ufw${NC}"
        echo "   ${BLUE}sudo apt install firewalld && sudo systemctl enable --now firewalld${NC}"
        echo ""
    elif ! command -v firewall-cmd &> /dev/null; then
        echo -e "${YELLOW}⚠ Firewalld is not installed — network isolation (restricted/allowlist modes) will not work.${NC}"
        echo "   Re-run this installer or set up manually: ${BLUE}sudo apt install firewalld && sudo systemctl enable --now firewalld && sudo firewall-cmd --permanent --add-masquerade && sudo firewall-cmd --reload${NC}"
        echo ""
    elif ! sudo -n firewall-cmd --query-masquerade &> /dev/null 2>&1; then
        echo -e "${YELLOW}⚠ Firewalld masquerade is not enabled — containers may not reach the internet.${NC}"
        echo "   Run: ${BLUE}sudo firewall-cmd --permanent --add-masquerade && sudo firewall-cmd --reload${NC}"
        echo ""
    fi

    echo "Documentation: https://github.com/${REPO}"
    echo ""
}

# Main installation
main() {
    echo ""
    echo -e "${BLUE}════════════════════════════════════════${NC}"
    echo -e "${BLUE}  claude-on-incus (coi) installer${NC}"
    echo -e "${BLUE}════════════════════════════════════════${NC}"
    echo ""

    detect_platform
    check_incus
    check_group
    check_ufw
    if [ "${SKIP_FIREWALLD:-0}" != "1" ]; then
        check_firewalld
    fi

    echo ""
    echo "Installation method:"
    echo "  1. Download pre-built binary (fastest)"
    echo "  2. Build from source"
    echo ""

    # Check if releases exist
    if curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" &> /dev/null; then
        prompt_choice "Choose [1/2] (default: 1): " "1"

        case $REPLY in
            2)
                build_from_source
                ;;
            *)
                download_binary
                ;;
        esac
    else
        echo -e "${YELLOW}⚠ No pre-built binaries available, building from source...${NC}"
        build_from_source
    fi

    post_install
}

# Handle errors
error_handler() {
    echo ""
    echo -e "${RED}✗ Installation failed${NC}"
    echo ""
    echo "If you need help:"
    echo "  - Check the documentation: https://github.com/${REPO}"
    echo "  - File an issue: https://github.com/${REPO}/issues"
    exit 1
}

trap error_handler ERR

# Run main
main "$@"
