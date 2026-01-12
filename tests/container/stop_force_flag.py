"""
Test for coi container stop --force - force stops container.

Tests that:
1. Launch a container
2. Stop with --force flag
3. Verify container stops quickly
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_stop_force_flag(coi_binary, cleanup_containers, workspace_dir):
    """
    Test force stopping a container.

    Flow:
    1. Launch a container
    2. Stop with --force flag
    3. Verify container stops
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

    # === Phase 2: Force stop container ===

    start_time = time.time()
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name, "--force"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    stop_time = time.time()

    assert result.returncode == 0, f"Container force stop should succeed. stderr: {result.stderr}"

    # Force stop should be relatively quick (less than 30 seconds)
    assert stop_time - start_time < 30, (
        f"Force stop should be quick, took {stop_time - start_time:.1f}s"
    )

    time.sleep(2)

    # === Phase 3: Verify not running ===

    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, "Force stopped container should not be running"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
