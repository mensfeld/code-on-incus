"""
Test for coi clean - removes stopped containers.

Tests that:
1. Launch a container
2. Stop it
3. Run coi clean
4. Verify stopped container is removed
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_clean_removes_stopped(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi clean removes stopped containers.

    Flow:
    1. Launch a container
    2. Stop it (but don't delete)
    3. Run coi clean --force
    4. Verify container is removed
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

    # Verify container is running
    containers = get_container_list()
    assert container_name in containers, f"Container {container_name} should be running"

    # === Phase 2: Stop container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 3: Clean ===

    result = subprocess.run(
        [coi_binary, "clean", "--force"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"coi clean should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 4: Verify container is removed ===

    containers = get_container_list()
    assert container_name not in containers, (
        f"Stopped container {container_name} should be removed by clean"
    )
