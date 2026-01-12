"""
Test for coi container running - returns true for running container.

Tests that:
1. Launch a container
2. Check running returns success
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_running_active_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that running returns success for a running container.

    Flow:
    1. Launch a container
    2. Check running returns 0 (true)
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

    # === Phase 2: Check running ===

    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Running should return 0 for active container"

    # === Phase 3: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
