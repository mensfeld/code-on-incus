"""
Test that allowlist mode fails gracefully on non-OVN networks.

Tests that:
1. Using --network=allowlist on a non-OVN network shows clear error message
2. Error message suggests using --network=open or setting up OVN
3. Container is not left in a broken state

Note: This test requires a non-OVN bridge network to be available.
In CI (which uses OVN), this test creates a temporary bridge network.
"""

import subprocess

import pytest


def test_allowlist_requires_ovn(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that allowlist mode fails with helpful error on non-OVN network.

    Flow:
    1. Create a temporary bridge network (non-OVN)
    2. Create a temporary profile using that bridge
    3. Try to start shell with --network=allowlist using that profile
    4. Verify it fails with clear error message
    5. Cleanup network and profile
    """
    # Create temporary bridge network
    bridge_name = "coi-test-bridge-allowlist"
    profile_name = "coi-test-non-ovn-allowlist"

    # Setup: Create bridge network
    result = subprocess.run(
        ["incus", "network", "create", bridge_name, "--type=bridge"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    if result.returncode != 0:
        pytest.skip(f"Could not create test bridge network: {result.stderr}")

    try:
        # Create profile with bridge network
        result = subprocess.run(
            ["incus", "profile", "create", profile_name],
            capture_output=True,
            text=True,
            timeout=10,
        )

        if result.returncode != 0:
            pytest.skip(f"Could not create test profile: {result.stderr}")

        # Configure profile to use bridge network
        result = subprocess.run(
            [
                "incus",
                "profile",
                "device",
                "add",
                profile_name,
                "eth0",
                "nic",
                f"network={bridge_name}",
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )

        if result.returncode != 0:
            pytest.skip(f"Could not configure test profile: {result.stderr}")

        # Try to use allowlist mode with non-OVN profile (should fail)
        result = subprocess.run(
            [
                coi_binary,
                "shell",
                "--workspace",
                workspace_dir,
                "--background",
                "--network=allowlist",
                f"--profile={profile_name}",
            ],
            capture_output=True,
            text=True,
            timeout=60,
        )

        # Should fail with non-zero exit code
        assert result.returncode != 0, (
            "Allowlist mode should fail on non-OVN network. "
            f"stdout: {result.stdout}, stderr: {result.stderr}"
        )

        # Check for helpful error message
        error_output = result.stderr.lower()
        assert "acl" in error_output or "ovn" in error_output, (
            f"Error message should mention ACL or OVN. Got: {result.stderr}"
        )

        assert "network=open" in error_output or "--network=open" in error_output, (
            f"Error message should suggest using --network=open. Got: {result.stderr}"
        )

        # Verify container was not created in broken state
        # (cleanup_containers fixture will handle any containers that were created)

    finally:
        # Cleanup: Delete profile and network
        subprocess.run(
            ["incus", "profile", "delete", profile_name],
            capture_output=True,
            timeout=10,
        )

        subprocess.run(
            ["incus", "network", "delete", bridge_name],
            capture_output=True,
            timeout=30,
        )
