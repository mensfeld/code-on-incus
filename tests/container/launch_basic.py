"""
Test for coi container launch - basic container launch.

Tests that:
1. Launch a container with image and name
2. Verify container is created and running
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_launch_basic(coi_binary, cleanup_containers, workspace_dir):
    """
    Test basic container launch with image and name.

    Flow:
    1. Launch a container with coi image
    2. Verify container is running
    3. Cleanup
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

    # === Phase 2: Verify container is running ===

    containers = get_container_list()
    assert container_name in containers, f"Container {container_name} should be in container list"

    # Check container status
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Container {container_name} should be running"

    # === Phase 3: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
