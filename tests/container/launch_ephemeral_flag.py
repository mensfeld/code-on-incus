"""
Test for coi container launch --ephemeral flag.

Tests that:
1. Launch a container with --ephemeral flag
2. Verify container is created
3. Verify container is deleted when stopped
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_launch_ephemeral_flag(coi_binary, cleanup_containers, workspace_dir):
    """
    Test container launch with --ephemeral flag.

    Flow:
    1. Launch container with --ephemeral
    2. Verify it's running
    3. Stop container
    4. Verify container is automatically deleted
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch ephemeral container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name, "--ephemeral"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, (
        f"Ephemeral container launch should succeed. stderr: {result.stderr}"
    )

    time.sleep(3)

    # === Phase 2: Verify container is running ===

    containers = get_container_list()
    assert container_name in containers, f"Ephemeral container {container_name} should be running"

    # === Phase 3: Stop container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    # Note: stop may return error if container auto-deleted
    time.sleep(3)

    # === Phase 4: Verify container is deleted ===

    containers = get_container_list()
    assert container_name not in containers, (
        f"Ephemeral container {container_name} should be auto-deleted after stop"
    )
