"""
Test for coi clean - keeps running containers.

Tests that:
1. Launch a container (keep it running)
2. Run coi clean
3. Verify running container is NOT removed
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_clean_keeps_running(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi clean does NOT remove running containers.

    Flow:
    1. Launch a container and keep it running
    2. Run coi clean --force
    3. Verify container is still running
    4. Cleanup
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

    # === Phase 2: Clean (should NOT remove running container) ===

    result = subprocess.run(
        [coi_binary, "clean", "--force"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"coi clean should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 3: Verify container is STILL running ===

    containers = get_container_list()
    assert container_name in containers, (
        f"Running container {container_name} should NOT be removed by clean"
    )

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    time.sleep(1)
    containers = get_container_list()
    assert container_name not in containers, (
        f"Container {container_name} should be deleted after cleanup"
    )
