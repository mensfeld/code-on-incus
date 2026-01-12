"""
Test for coi container exists - returns true for existing container.

Tests that:
1. Launch a container
2. Check exists returns success
3. Verify for both running and stopped states
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_exists_running_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that exists returns success for existing container.

    Flow:
    1. Launch a container
    2. Check exists returns 0 (true)
    3. Stop container
    4. Check exists still returns 0 (container exists but stopped)
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

    # === Phase 2: Check exists (running) ===

    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Exists should return 0 for running container"

    # === Phase 3: Stop container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 4: Check exists (stopped) ===

    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Exists should return 0 for stopped container (it still exists)"

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
