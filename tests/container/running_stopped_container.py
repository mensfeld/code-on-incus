"""
Test for coi container running - returns false for stopped container.

Tests that:
1. Launch a container
2. Stop it
3. Check running returns non-zero (false)
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_running_stopped_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that running returns non-zero for a stopped container.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Check running returns non-zero (false)
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

    # Verify running
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Container should be running initially"

    # === Phase 2: Stop container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 3: Check running ===

    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, "Running should return non-zero for stopped container"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
