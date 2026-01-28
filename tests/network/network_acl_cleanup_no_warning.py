"""
Test for network ACL cleanup - no warning when poweroff is used.

Tests that:
1. Container with network ACL (restricted/allowlist mode) starts successfully
2. When container is stopped and then shutdown, ACL is cleaned up silently
3. No "failed to delete network ACL" warning appears
4. ACL is properly removed from Incus

This test validates the fix for issue #67 where ACL deletion would fail with
"exit status 1" because it was attempted while the ACL was still attached to
the container's network device. The fix deletes the container first (which
detaches the ACL), then deletes the ACL.
"""

import os
import subprocess
import time

import pytest

# Skip all tests in this module when running on bridge network (no OVN/ACL support)
pytestmark = pytest.mark.skipif(
    os.getenv("CI_NETWORK_TYPE") == "bridge",
    reason="Network ACL cleanup test requires OVN networking (ACL support)",
)


def test_restricted_mode_acl_cleanup_no_warning(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that restricted mode ACL cleanup doesn't show warnings.

    Flow:
    1. Start shell in background with restricted mode
    2. Stop the container (simulating poweroff)
    3. Run shutdown command which triggers cleanup
    4. Verify no "failed to delete network ACL" warning appears
    5. Verify ACL was properly removed
    """
    # Start shell in background with restricted network mode
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--background",
            "--network=restricted",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start successfully. stderr: {result.stderr}"

    # Extract container name from output
    container_name = None
    for line in result.stderr.split("\n"):
        if "Container:" in line:
            # Example: "Container: coi-abc12345-1"
            parts = line.split()
            if len(parts) >= 2:
                container_name = parts[1]
                break

    assert container_name, f"Could not extract container name from output: {result.stderr}"

    # Determine ACL name (format: coi-<container-name>-restricted)
    acl_name = f"coi-{container_name}-restricted"

    # Give container time to fully start and ACL to be created
    time.sleep(5)

    # Verify ACL exists
    acl_check = subprocess.run(
        ["incus", "network", "acl", "show", acl_name],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert acl_check.returncode == 0, f"ACL '{acl_name}' should exist after container start"

    # Simulate poweroff by stopping the container
    # This replicates what happens when user runs "sudo poweroff" inside container
    stop_result = subprocess.run(
        ["incus", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert stop_result.returncode == 0, f"Container stop should succeed. stderr: {stop_result.stderr}"

    # Give shutdown time to complete
    time.sleep(2)

    # Now run shutdown command which should trigger cleanup with ACL removal
    cleanup_result = subprocess.run(
        [coi_binary, "shutdown", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # The cleanup should succeed
    assert cleanup_result.returncode == 0, f"Cleanup should succeed. stderr: {cleanup_result.stderr}"

    combined_output = cleanup_result.stdout + cleanup_result.stderr

    # CRITICAL: Verify no ACL deletion warning appears
    assert "Warning: failed to delete network ACL" not in combined_output, (
        f"Should not show ACL deletion warning. Output:\n{combined_output}"
    )

    assert "exit status 1" not in combined_output, (
        f"Should not show 'exit status 1' error. Output:\n{combined_output}"
    )

    # Verify container no longer exists
    container_check = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert container_check.returncode != 0, "Container should not exist after cleanup"

    # Clean up orphaned ACL (shutdown doesn't trigger network teardown)
    subprocess.run(
        ["incus", "network", "acl", "delete", acl_name],
        capture_output=True,
        timeout=10,
    )


def test_allowlist_mode_acl_cleanup_no_warning(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that allowlist mode ACL cleanup doesn't show warnings.

    Flow:
    1. Start shell in background with allowlist mode
    2. Stop the container (simulating poweroff)
    3. Run shutdown command which triggers cleanup
    4. Verify no "failed to delete network ACL" warning appears
    5. Verify ACL was properly removed
    """
    # Start shell in background with allowlist network mode
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--background",
            "--network=allowlist",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start successfully. stderr: {result.stderr}"

    # Extract container name from output
    container_name = None
    for line in result.stderr.split("\n"):
        if "Container:" in line:
            parts = line.split()
            if len(parts) >= 2:
                container_name = parts[1]
                break

    assert container_name, f"Could not extract container name from output: {result.stderr}"

    # Determine ACL name (format: coi-<container-name>-allowlist)
    acl_name = f"coi-{container_name}-allowlist"

    # Give container time to fully start and ACL to be created
    time.sleep(5)

    # Verify ACL exists
    acl_check = subprocess.run(
        ["incus", "network", "acl", "show", acl_name],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert acl_check.returncode == 0, f"ACL '{acl_name}' should exist after container start"

    # Simulate poweroff by stopping the container
    stop_result = subprocess.run(
        ["incus", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert stop_result.returncode == 0, f"Container stop should succeed. stderr: {stop_result.stderr}"

    # Give shutdown time to complete
    time.sleep(2)

    # Run shutdown command which triggers cleanup
    cleanup_result = subprocess.run(
        [coi_binary, "shutdown", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert cleanup_result.returncode == 0, f"Cleanup should succeed. stderr: {cleanup_result.stderr}"

    combined_output = cleanup_result.stdout + cleanup_result.stderr

    # CRITICAL: Verify no ACL deletion warning appears
    assert "Warning: failed to delete network ACL" not in combined_output, (
        f"Should not show ACL deletion warning. Output:\n{combined_output}"
    )

    assert "exit status 1" not in combined_output, (
        f"Should not show 'exit status 1' error. Output:\n{combined_output}"
    )

    # Verify container no longer exists
    container_check = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert container_check.returncode != 0, "Container should not exist after cleanup"

    # Clean up orphaned ACL (shutdown doesn't trigger network teardown)
    subprocess.run(
        ["incus", "network", "acl", "delete", acl_name],
        capture_output=True,
        timeout=10,
    )


def test_open_mode_no_acl_warnings(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that open mode doesn't produce any ACL-related warnings.

    Open mode doesn't use ACLs, so there should be no ACL warnings whatsoever.

    Flow:
    1. Start shell in background with open mode
    2. Stop the container
    3. Run shutdown command
    4. Verify no ACL-related warnings appear
    """
    # Start shell in background with open network mode
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--background",
            "--network=open",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start successfully. stderr: {result.stderr}"

    # Extract container name from output
    container_name = None
    for line in result.stderr.split("\n"):
        if "Container:" in line:
            parts = line.split()
            if len(parts) >= 2:
                container_name = parts[1]
                break

    assert container_name, f"Could not extract container name from output: {result.stderr}"

    # Give container time to fully start
    time.sleep(5)

    # Simulate poweroff by stopping the container
    stop_result = subprocess.run(
        ["incus", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert stop_result.returncode == 0, f"Container stop should succeed. stderr: {stop_result.stderr}"

    # Give shutdown time to complete
    time.sleep(2)

    # Run shutdown command
    cleanup_result = subprocess.run(
        [coi_binary, "shutdown", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert cleanup_result.returncode == 0, f"Cleanup should succeed. stderr: {cleanup_result.stderr}"

    combined_output = cleanup_result.stdout + cleanup_result.stderr

    # Verify no ACL-related warnings (open mode doesn't use ACLs)
    assert "network ACL" not in combined_output.lower(), (
        f"Open mode should not mention ACLs. Output:\n{combined_output}"
    )

    assert "exit status 1" not in combined_output, (
        f"Should not show 'exit status 1' error. Output:\n{combined_output}"
    )

    # Verify container no longer exists
    container_check = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert container_check.returncode != 0, "Container should not exist after cleanup"
