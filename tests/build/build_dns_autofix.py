"""
Test for coi build - DNS auto-fix functionality.

Tests that:
1. When DNS is misconfigured (127.0.0.53 stub resolver), build auto-detects and fixes it
2. Build completes successfully with auto-fix
3. Warning message is displayed about the DNS misconfiguration

This test temporarily modifies the Incus network configuration to simulate
the DNS misconfiguration that occurs on Ubuntu systems with systemd-resolved.
"""

import subprocess

import pytest


def get_incus_network():
    """Get the name of the Incus bridge network."""
    result = subprocess.run(
        ["incus", "network", "list", "--format=csv"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    if result.returncode != 0:
        return None

    # Find a managed bridge network (usually incusbr0)
    for line in result.stdout.strip().split("\n"):
        if not line:
            continue
        parts = line.split(",")
        if len(parts) >= 3 and parts[1] == "bridge" and parts[2] == "YES":
            return parts[0]
    return None


def break_dns_config(network_name):
    """Configure Incus network to push broken DNS (127.0.0.53) to containers."""
    result = subprocess.run(
        ["incus", "network", "set", network_name, "raw.dnsmasq", "dhcp-option=6,127.0.0.53"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    return result.returncode == 0


def restore_dns_config(network_name):
    """Remove the broken DNS configuration from Incus network."""
    subprocess.run(
        ["incus", "network", "unset", network_name, "raw.dnsmasq"],
        capture_output=True,
        timeout=30,
        check=False,
    )


def test_build_dns_autofix(coi_binary, tmp_path):
    """
    Test that build auto-fixes DNS misconfiguration.

    This test builds from a fresh Ubuntu base image (not from coi) to ensure
    the DNS auto-fix is triggered. Building from coi would inherit the already-
    fixed DNS configuration.

    Flow:
    1. Get Incus network name
    2. Break DNS configuration (set 127.0.0.53)
    3. Clean up any existing build container
    4. Run coi build custom with --base images:ubuntu/22.04
    5. Verify build succeeds
    6. Verify DNS auto-fix messages appear in output
    7. Restore DNS configuration
    """
    # Get network name
    network_name = get_incus_network()
    if not network_name:
        pytest.skip("Could not determine Incus network name")

    image_name = "coi-test-dns-autofix"

    # Create minimal build script that verifies DNS works
    build_script = tmp_path / "build.sh"
    build_script.write_text(
        """#!/bin/bash
set -e
echo "Testing DNS resolution..."
# This should work after auto-fix
if getent hosts archive.ubuntu.com > /dev/null 2>&1; then
    echo "DNS resolution works!"
else
    echo "DNS resolution failed!"
    exit 1
fi
"""
    )

    try:
        # Break DNS configuration
        if not break_dns_config(network_name):
            pytest.skip("Could not modify Incus network configuration (permission denied?)")

        # Clean up any existing build container
        subprocess.run(
            ["incus", "delete", "--force", "coi-build"],
            capture_output=True,
            timeout=30,
            check=False,
        )

        # Build custom image from fresh Ubuntu base (not coi) to trigger DNS fix
        # Using --base images:ubuntu/22.04 ensures we start with broken DNS
        result = subprocess.run(
            [coi_binary, "build", "custom", image_name,
             "--base", "images:ubuntu/22.04",
             "--script", str(build_script)],
            capture_output=True,
            text=True,
            timeout=600,  # Longer timeout for DNS fix + build
        )

        combined_output = result.stdout + result.stderr

        # Build should succeed despite broken DNS
        assert result.returncode == 0, (
            f"Build should succeed with DNS auto-fix. "
            f"Exit code: {result.returncode}\n"
            f"Output:\n{combined_output}"
        )

        # Verify DNS auto-fix was applied
        assert "Detected DNS misconfiguration" in combined_output or "DNS configuration fixed" in combined_output, (
            f"Build should show DNS auto-fix message. Output:\n{combined_output}"
        )

        # Verify the build script's DNS check passed
        assert "DNS resolution works!" in combined_output, (
            f"DNS should work after auto-fix. Output:\n{combined_output}"
        )

    finally:
        # Always restore DNS configuration
        restore_dns_config(network_name)

        # Cleanup test image
        subprocess.run(
            [coi_binary, "image", "delete", image_name],
            capture_output=True,
            timeout=30,
            check=False,
        )


def test_build_with_working_dns_no_changes(coi_binary, tmp_path):
    """
    Test that build doesn't modify DNS when it's already working.

    This ensures the auto-fix is conditional and doesn't break
    properly configured systems.

    Flow:
    1. Ensure DNS is working (restore if needed)
    2. Run coi build custom
    3. Verify no DNS modification messages appear
    """
    # Get network name
    network_name = get_incus_network()
    if network_name:
        # Ensure DNS is not broken from previous test
        restore_dns_config(network_name)

    # Skip if coi base image doesn't exist
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi"],
        capture_output=True,
    )
    if result.returncode != 0:
        pytest.skip("coi image not built - run 'coi build' first")

    image_name = "coi-test-dns-nochange"

    # Create minimal build script
    build_script = tmp_path / "build.sh"
    build_script.write_text(
        """#!/bin/bash
set -e
echo "Build with working DNS"
"""
    )

    try:
        # Clean up any existing build container
        subprocess.run(
            ["incus", "delete", "--force", "coi-build"],
            capture_output=True,
            timeout=30,
            check=False,
        )

        # Build custom image
        result = subprocess.run(
            [coi_binary, "build", "custom", image_name, "--script", str(build_script)],
            capture_output=True,
            text=True,
            timeout=300,
        )

        combined_output = result.stdout + result.stderr

        # Build should succeed
        assert result.returncode == 0, (
            f"Build should succeed. Exit code: {result.returncode}\n"
            f"Output:\n{combined_output}"
        )

        # Should NOT see DNS fix messages (DNS was already working)
        assert "Detected DNS misconfiguration" not in combined_output, (
            f"Build should not show DNS fix message when DNS works. Output:\n{combined_output}"
        )

    finally:
        # Cleanup test image
        subprocess.run(
            [coi_binary, "image", "delete", image_name],
            capture_output=True,
            timeout=30,
            check=False,
        )
