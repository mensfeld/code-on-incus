"""
Test for coi kill - kill a stopped container.

Tests that:
1. Launch a container
2. Stop it
3. Kill it while stopped
4. Verify it's deleted
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_kill_stopped_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test killing a stopped container.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Kill the container
    4. Verify it's deleted
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

    # === Phase 3: Kill container ===

    result = subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Kill should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Killed" in combined_output or "killed" in combined_output.lower(), (
        f"Should show killed confirmation. Got:\n{combined_output}"
    )

    # === Phase 4: Verify deleted ===

    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, f"Container should not exist after kill. stdout: {result.stdout}"
