"""
Test for coi kill - kill multiple containers at once.

Tests that:
1. Launch multiple containers
2. Kill them all in one command with --force
3. Verify all are deleted
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_kill_multiple_containers(coi_binary, cleanup_containers, workspace_dir):
    """
    Test killing multiple containers at once.

    Flow:
    1. Launch 2 containers
    2. Kill both with one command
    3. Verify both are deleted
    """
    container1 = calculate_container_name(workspace_dir, 1)
    container2 = calculate_container_name(workspace_dir, 2)

    # === Phase 1: Launch containers ===

    for container_name in [container1, container2]:
        result = subprocess.run(
            [coi_binary, "container", "launch", "coi", container_name],
            capture_output=True,
            text=True,
            timeout=120,
        )
        assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 2: Kill both containers with --force (skip confirmation) ===

    result = subprocess.run(
        [coi_binary, "kill", "--force", container1, container2],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Kill should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Killed 2" in combined_output or "killed" in combined_output.lower(), (
        f"Should show killed count. Got:\n{combined_output}"
    )

    # === Phase 3: Verify both deleted ===

    for container_name in [container1, container2]:
        result = subprocess.run(
            [coi_binary, "container", "exists", container_name],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert result.returncode != 0, f"Container {container_name} should not exist after kill"
