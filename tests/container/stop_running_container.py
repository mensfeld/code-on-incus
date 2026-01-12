"""
Test for coi container stop - stops a running container.

Tests that:
1. Launch a container
2. Stop it
3. Verify it's no longer running
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_stop_running_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test stopping a running container.

    Flow:
    1. Launch a container
    2. Verify it's running
    3. Stop the container
    4. Verify it's no longer running
    5. Cleanup
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

    # === Phase 2: Stop container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 3: Verify not running ===

    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, "Stopped container should not be running"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
