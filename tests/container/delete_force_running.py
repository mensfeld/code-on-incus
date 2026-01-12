"""
Test for coi container delete --force - force deletes running container.

Tests that:
1. Launch a container (keep it running)
2. Delete with --force flag
3. Verify container is removed
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_delete_force_running(coi_binary, cleanup_containers, workspace_dir):
    """
    Test force deleting a running container.

    Flow:
    1. Launch a container (keep it running)
    2. Delete with --force flag
    3. Verify container is removed
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

    # Verify running
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Container should be running"

    # === Phase 2: Force delete ===

    result = subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Force delete should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 3: Verify removed ===

    containers = get_container_list()
    assert container_name not in containers, "Force deleted container should not exist"
