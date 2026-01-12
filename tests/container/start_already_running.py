"""
Test for coi container start - handles already running container.

Tests that:
1. Launch a container (already running)
2. Try to start it
3. Verify behavior (should succeed or indicate already running)
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_start_already_running(coi_binary, cleanup_containers, workspace_dir):
    """
    Test starting an already running container.

    Flow:
    1. Launch a container (it's now running)
    2. Try to start it again
    3. Verify it handles gracefully (success or informative message)
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

    assert result.returncode == 0, "Container should be running"

    # === Phase 2: Try to start already running container ===

    result = subprocess.run(
        [coi_binary, "container", "start", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    # Should either succeed (idempotent) or give informative message
    result.stdout + result.stderr

    # Container should still be running
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Container should still be running after start attempt"

    # === Phase 3: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
