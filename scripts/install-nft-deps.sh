#!/bin/bash
# Install dependencies for nftables monitoring

set -e

echo "Installing nftables monitoring dependencies..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    SUDO="sudo"
else
    SUDO=""
fi

# Install systemd development headers
echo "Installing libsystemd-dev..."
$SUDO apt-get update
$SUDO apt-get install -y libsystemd-dev

# Install nftables
echo "Installing nftables..."
$SUDO apt-get install -y nftables

# Add user to systemd-journal group for reading logs without sudo
echo "Adding $USER to systemd-journal group..."
$SUDO usermod -a -G systemd-journal $USER

# Create sudoers file for nftables (NOPASSWD)
echo "Creating sudoers file for nftables..."
$SUDO tee /etc/sudoers.d/coi <<'EOF'
# COI nftables monitoring (NOPASSWD for specific commands)
%incus-admin ALL=(ALL) NOPASSWD: /usr/sbin/nft list *
%incus-admin ALL=(ALL) NOPASSWD: /usr/sbin/nft add rule *
%incus-admin ALL=(ALL) NOPASSWD: /usr/sbin/nft delete rule *
%incus-admin ALL=(ALL) NOPASSWD: /usr/sbin/nft -a list *
EOF

$SUDO chmod 0440 /etc/sudoers.d/coi

echo ""
echo "✓ Dependencies installed successfully!"
echo ""
echo "IMPORTANT: You must log out and log back in (or run 'newgrp systemd-journal')"
echo "           for the systemd-journal group membership to take effect."
echo ""
echo "To verify setup:"
echo "  1. Test journal access: journalctl -k -n 10"
echo "  2. Test nftables access: sudo -n nft list ruleset"
