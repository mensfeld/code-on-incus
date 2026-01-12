"""
Test for coi container delete - fails for running container without --force.

Tests that:
1. Launch a container (keep it running)
2. Try to delete without --force
3. Verify command fails
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_delete_running_fails(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that deleting running container without --force fails.

    Flow:
    1. Launch a container (keep it running)
    2. Try to delete without --force
    3. Verify it fails
    4. Verify container still exists
    5. Cleanup with --force
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

    # === Phase 2: Try to delete without --force ===

    result = subprocess.run(
        [coi_binary, "container", "delete", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    # === Phase 3: Verify failure ===

    assert result.returncode != 0, "Deleting running container without --force should fail"

    # === Phase 4: Verify container still exists ===

    containers = get_container_list()
    assert container_name in containers, "Container should still exist after failed delete"

    # Still running
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Container should still be running after failed delete"

    # === Phase 5: Cleanup with --force ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
