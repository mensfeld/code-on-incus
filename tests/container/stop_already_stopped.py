"""
Test for coi container stop - handles already stopped container.

Tests that:
1. Launch a container
2. Stop it
3. Try to stop again
4. Verify behavior (should handle gracefully)
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_stop_already_stopped(coi_binary, cleanup_containers, workspace_dir):
    """
    Test stopping an already stopped container.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Try to stop again
    4. Verify it handles gracefully
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

    # === Phase 2: Stop container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # Verify not running
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, "Stopped container should not be running"

    # === Phase 3: Try to stop again ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    # Should either succeed (idempotent) or give informative message
    # Container should still exist (just stopped)
    containers = get_container_list()
    assert container_name in containers, "Container should still exist after double stop"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
