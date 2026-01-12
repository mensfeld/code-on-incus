"""
Test for coi container delete - deletes a stopped container.

Tests that:
1. Launch a container
2. Stop it
3. Delete it
4. Verify it's removed
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_delete_stopped_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test deleting a stopped container.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Delete the container
    4. Verify it's removed from the list
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 2: Stop container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # Verify container still exists
    containers = get_container_list()
    assert container_name in containers, "Stopped container should still exist"

    # === Phase 3: Delete container ===

    result = subprocess.run(
        [coi_binary, "container", "delete", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Container delete should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 4: Verify removed ===

    containers = get_container_list()
    assert container_name not in containers, "Deleted container should not exist"
