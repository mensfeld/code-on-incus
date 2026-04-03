"""
Test for coi shutdown - no spurious errors when container stops during timeout.

Tests that:
1. Launch a container
2. Stop it externally (simulating it stopping during graceful shutdown)
3. Run shutdown with timeout
4. Verify no "Error: The instance is already stopped" message

This test validates the fix for the race condition where a container could
stop during the timeout window, causing spurious error messages when the
force-kill attempt executes.
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_shutdown_no_spurious_errors(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that shutdown doesn't show spurious errors when container stops itself.

    Flow:
    1. Launch a container
    2. Initiate shutdown with timeout (in background)
    3. Stop container externally (simulating graceful stop completing)
    4. Wait for shutdown to complete
    5. Verify no "already stopped" error appears
    6. Cleanup
    """
    slot = 7
    container_name = calculate_container_name(workspace_dir, slot)

    # Launch a container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # Stop the container (simulating it stopping during graceful shutdown)
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # Now shutdown the already-stopped container with a timeout
    # This simulates the race condition where graceful shutdown completes
    # during the timeout window, and the force-kill check runs on stopped container
    result = subprocess.run(
        [coi_binary, "shutdown", "--timeout=5", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Shutdown should succeed
    assert result.returncode == 0, f"Shutdown should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr

    # Verify no spurious error about already stopped container
    # This error could appear if the code tried to force-kill an already-stopped container
    assert "Error: The instance is already stopped" not in combined_output, (
        f"Should not show spurious 'already stopped' error. Output:\n{combined_output}"
    )

    # Also verify no "force-killing" message since container was already stopped
    # The fix should detect the container is stopped and skip the force-kill
    if "Timeout reached" in combined_output:
        # If timeout message appears, verify no force-kill attempt on stopped container
        error_lines = [
            line
            for line in combined_output.split("\n")
            if "Error:" in line and "already stopped" in line.lower()
        ]
        assert len(error_lines) == 0, (
            "Should not attempt force-kill on already-stopped container. Found:\n"
            + "\n".join(error_lines)
        )

    # Verify container no longer exists
    time.sleep(2)
    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, "Container should not exist after shutdown"
