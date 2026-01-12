"""
Test for coi shutdown - verifies container is deleted.

Tests that:
1. Launch a container
2. Shutdown it
3. Verify container no longer exists with multiple checks
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_shutdown_verifies_deletion(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that shutdown properly deletes the container.

    Flow:
    1. Launch a container
    2. Run coi shutdown <container>
    3. Verify with 'container exists' that it's gone
    4. Verify it doesn't appear in 'coi list'
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

    # Verify container exists before shutdown
    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Container should exist before shutdown"

    # Shutdown the container
    result = subprocess.run(
        [coi_binary, "shutdown", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Shutdown should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # Verify container doesn't exist
    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Container should not exist after shutdown"

    # Also verify it doesn't appear in list
    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert container_name not in result.stdout, (
        f"Container should not appear in list. Got:\n{result.stdout}"
    )
