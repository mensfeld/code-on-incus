"""
Test for coi kill - kill a running container.

Tests that:
1. Launch a container
2. Kill it while running
3. Verify it's deleted
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_kill_running_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test killing a running container.

    Flow:
    1. Launch a container
    2. Verify it's running
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

    # === Phase 2: Verify running ===

    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Container should be running. stderr: {result.stderr}"

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
