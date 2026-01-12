"""
Test for coi container start - starts a stopped container.

Tests that:
1. Launch a container
2. Stop it
3. Start it again
4. Verify it's running
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_start_stopped_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test starting a stopped container.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Start the container
    4. Verify it's running again
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

    # === Phase 3: Start container ===

    result = subprocess.run(
        [coi_binary, "container", "start", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Container start should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 4: Verify running ===

    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Started container should be running"

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
