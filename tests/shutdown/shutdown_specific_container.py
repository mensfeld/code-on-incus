"""
Test for coi shutdown - shutdown a specific container.

Tests that:
1. Launch a container
2. Shutdown by name
3. Verify container is deleted
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_shutdown_specific_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test shutting down a specific container by name.

    Flow:
    1. Launch a container
    2. Run coi shutdown <container-name>
    3. Verify container is deleted
    """
    slot = 1
    container_name = calculate_container_name(workspace_dir, slot)

    # Launch a container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # Shutdown the container
    result = subprocess.run(
        [coi_binary, "shutdown", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Shutdown should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "shutdown" in combined_output.lower(), (
        f"Should show shutdown message. Got:\n{combined_output}"
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
