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

# Detect package manager
detect_pkg_manager() {
    if command -v apt-get &> /dev/null; then
        PKG_MANAGER="apt"
    elif command -v pacman &> /dev/null; then
        PKG_MANAGER="pacman"
    elif command -v dnf &> /dev/null; then
        PKG_MANAGER="dnf"
    elif command -v zypper &> /dev/null; then
        PKG_MANAGER="zypper"
    else
        PKG_MANAGER="unknown"
    fi
}

# Install a package using the detected package manager
# Usage: pkg_install <apt-name> [pacman-name] [dnf-name] [zypper-name]
# If a distro-specific name is omitted, the apt name is used as fallback.
pkg_install() {
    local apt_name="$1"
    local pacman_name="${2:-$apt_name}"
    local dnf_name="${3:-$apt_name}"
    local zypper_name="${4:-$apt_name}"

    case "$PKG_MANAGER" in
        apt)    sudo apt-get install -y "$apt_name" ;;
        pacman) sudo pacman -S --noconfirm "$pacman_name" ;;
        dnf)    sudo dnf install -y "$dnf_name" ;;
        zypper) sudo zypper install -y "$zypper_name" ;;
        *)
            echo -e "${RED}✗ Unknown package manager — install '$apt_name' manually${NC}"
            return 1
            ;;
    esac
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
            echo "  See: https://github.com/mensfeld/code-on-incus/wiki/macOS-Setup-Guide"
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
        echo "  code-on-incus requires Incus to be installed."
        echo "  Install Incus: https://linuxcontainers.org/incus/docs/main/installing/"
        echo ""
        echo "  Quick install examples:"
        echo "    Ubuntu/Debian: sudo apt install -y incus"
        echo "    Arch Linux:    sudo pacman -S incus"
        echo "    Fedora:        sudo dnf install incus"
        echo ""
        echo "  Then: sudo incus admin init --auto"
        echo "        sudo usermod -aG incus-admin \$USER"
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
        echo "  You need to be in the incus-admin group to use code-on-incus."
        echo "  Run: sudo usermod -aG incus-admin \$USER"
        echo "  Then log out and back in for changes to take effect."
        echo ""
    fi
}

# Set up passwordless sudo for nft (required for network isolation)
setup_nft_sudoers() {
    local nft_path
    nft_path="$(command -v nft 2>/dev/null)"
    if [ -z "$nft_path" ]; then
        return
    fi

    # Already configured? Check for the sudoers drop-in directly so we don't
    # get a false positive from a cached sudo timestamp.
    if [ -f /etc/sudoers.d/coi-nft ]; then
        echo -e "${GREEN}✓ Passwordless sudo for nft already configured${NC}"
        return
    fi

    echo -e "${BLUE}→ Configuring passwordless sudo for nft...${NC}"
    echo "$USER ALL=(ALL) NOPASSWD: $nft_path" | sudo tee /etc/sudoers.d/coi-nft > /dev/null
    sudo chmod 0440 /etc/sudoers.d/coi-nft
    echo -e "${GREEN}✓ Passwordless sudo configured for nft${NC}"
}

# Check nftables availability for network isolation
check_nft() {
    echo -e "${BLUE}→ Checking nftables (for network isolation)...${NC}"

    if ! command -v nft &> /dev/null; then
        echo -e "${YELLOW}⚠ nft not found${NC}"
        echo ""
        echo "  Network isolation (restricted/allowlist modes) requires nftables."
        echo "  Without it, you can still use open mode."
        echo ""

        if [ "$NONINTERACTIVE" = "1" ]; then
            echo -e "${BLUE}→ Non-interactive mode: installing nftables...${NC}"
            pkg_install nftables
        else
            read -p "  Install nftables now? [Y/n] " -n 1 -r </dev/tty
            echo ""
            if [[ ! $REPLY =~ ^[Nn]$ ]]; then
                echo -e "${BLUE}→ Installing nftables...${NC}"
                pkg_install nftables
            fi
        fi
    else
        echo -e "${GREEN}✓ nft is installed${NC}"
    fi

    setup_nft_sudoers
}

# Download binary from GitHub releases
download_binary() {
    local download_url
    local tmp_dir
    local binary_path

    echo -e "${BLUE}→ Downloading code-on-incus...${NC}"

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
    else
        sudo cp "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
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
    else
        sudo cp "$built_binary" "${INSTALL_DIR}/${BINARY_NAME}"
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

# Ensure the Incus systemd service is enabled and running
ensure_incus_service() {
    if ! command -v incus &> /dev/null; then
        return
    fi

    # Check if incus service (or socket) is active
    if systemctl is-active --quiet incus.service 2>/dev/null || systemctl is-active --quiet incus.socket 2>/dev/null; then
        echo -e "${GREEN}✓ Incus service is running${NC}"
        return
    fi

    echo -e "${BLUE}→ Enabling and starting Incus service...${NC}"
    if sudo systemctl enable --now incus.service 2>/dev/null; then
        echo -e "${GREEN}✓ Incus service enabled and started${NC}"
    elif sudo systemctl enable --now incus.socket 2>/dev/null; then
        echo -e "${GREEN}✓ Incus socket enabled and started${NC}"
    else
        echo -e "${YELLOW}⚠ Could not start Incus service${NC}"
        echo "  Try manually: sudo systemctl enable --now incus.service"
    fi
}

# Ensure subordinate UID/GID ranges are configured for unprivileged containers
ensure_idmap() {
    if ! command -v incus &> /dev/null; then
        return
    fi

    echo -e "${BLUE}→ Checking subordinate UID/GID mapping...${NC}"

    local needs_fix=0

    # Check if root has a subuid range with at least 65536 UIDs
    if [ -f /etc/subuid ] && grep -qE '^root:[0-9]+:[0-9]{5,}' /etc/subuid; then
        echo -e "${GREEN}✓ /etc/subuid has root mapping${NC}"
    else
        echo -e "${YELLOW}⚠ /etc/subuid missing root subordinate range${NC}"
        needs_fix=1
    fi

    if [ -f /etc/subgid ] && grep -qE '^root:[0-9]+:[0-9]{5,}' /etc/subgid; then
        echo -e "${GREEN}✓ /etc/subgid has root mapping${NC}"
    else
        echo -e "${YELLOW}⚠ /etc/subgid missing root subordinate range${NC}"
        needs_fix=1
    fi

    if [ "$needs_fix" = "0" ]; then
        return
    fi

    echo ""
    echo "  Incus needs subordinate UID/GID ranges for unprivileged containers."
    echo "  Without this, container launches will fail with:"
    echo "    \"System doesn't have a functional idmap setup\""
    echo ""

    if [ "$NONINTERACTIVE" = "1" ]; then
        echo -e "${BLUE}→ Non-interactive mode: configuring idmap...${NC}"
    else
        read -p "  Configure subordinate UID/GID ranges now? [Y/n] " -n 1 -r </dev/tty
        echo ""
        if [[ $REPLY =~ ^[Nn]$ ]]; then
            return
        fi
    fi

    if ! grep -qE '^root:[0-9]+:[0-9]{5,}' /etc/subuid 2>/dev/null; then
        echo "root:1000000:1000000000" | sudo tee -a /etc/subuid > /dev/null
        echo -e "${GREEN}✓ Added root range to /etc/subuid${NC}"
    fi
    if ! grep -qE '^root:[0-9]+:[0-9]{5,}' /etc/subgid 2>/dev/null; then
        echo "root:1000000:1000000000" | sudo tee -a /etc/subgid > /dev/null
        echo -e "${GREEN}✓ Added root range to /etc/subgid${NC}"
    fi

    # Restart Incus to pick up new mappings
    if systemctl is-active --quiet incus.service 2>/dev/null; then
        echo -e "${BLUE}→ Restarting Incus to apply idmap changes...${NC}"
        sudo systemctl restart incus.service
        echo -e "${GREEN}✓ Incus restarted${NC}"
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
        if ! pkg_install zfsutils-linux zfs-utils zfs zfs 2>/dev/null; then
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

# Fetch detection databases (GTFOBins + Sigma)
fetch_detection_databases() {
    if ! command -v coi &> /dev/null; then
        return
    fi

    echo ""
    echo -e "${BLUE}→ Fetching detection databases (GTFOBins + Sigma)...${NC}"
    echo "  This clones the GTFOBins reverse-shell database and Sigma linux/process_creation"
    echo "  rules used by the monitoring daemon. The Sigma clone is sparse (~300 KB)."
    echo ""
    if coi update patterns; then
        echo -e "${GREEN}✓ Detection databases fetched${NC}"
    else
        echo -e "${YELLOW}⚠ Detection database fetch failed (requires git and network access)${NC}"
        echo "  Run manually later: ${BLUE}coi update patterns${NC}"
    fi
}

# Post-install setup
post_install() {
    ensure_incus_service || true
    ensure_idmap || true
    ensure_incus_initialized || true

    # Try to set up ZFS storage (best-effort, don't abort installer on failure)
    setup_zfs_storage || true

    # Fetch GTFOBins and Sigma detection databases
    fetch_detection_databases || true

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

    if ! command -v nft &> /dev/null; then
        echo -e "${YELLOW}⚠ nftables is not installed — network isolation (restricted/allowlist modes) will not work.${NC}"
        echo "   Install with: ${BLUE}sudo apt install nftables${NC}"
        echo ""
    elif ! [ -f /etc/sudoers.d/coi-nft ]; then
        echo -e "${YELLOW}⚠ Passwordless sudo for nft not configured — network isolation will not work.${NC}"
        echo "   Run: ${BLUE}echo \"\$USER ALL=(ALL) NOPASSWD: \$(command -v nft)\" | sudo tee /etc/sudoers.d/coi-nft && sudo chmod 0440 /etc/sudoers.d/coi-nft${NC}"
        echo ""
    fi

    echo "Documentation: https://github.com/${REPO}"
    echo ""
}

# Main installation
main() {
    echo ""
    echo -e "${BLUE}════════════════════════════════════════${NC}"
    echo -e "${BLUE}  code-on-incus (coi) installer${NC}"
    echo -e "${BLUE}════════════════════════════════════════${NC}"
    echo ""

    detect_platform
    detect_pkg_manager
    check_incus
    check_group
    check_nft

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
