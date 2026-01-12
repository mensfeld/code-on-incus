"""
Test for coi shutdown - stopped container.

Tests that:
1. Launch and stop a container
2. Shutdown the stopped container
3. Verify container is deleted
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_shutdown_stopped_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test shutting down a container that is already stopped.

    Flow:
    1. Launch a container
    2. Stop it
    3. Run coi shutdown <container>
    4. Verify container is deleted
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

    # Stop the container first
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # Verify container exists but is stopped
    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Container should still exist after stop"

    # Shutdown the stopped container
    result = subprocess.run(
        [coi_binary, "shutdown", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, (
        f"Shutdown of stopped container should succeed. stderr: {result.stderr}"
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
