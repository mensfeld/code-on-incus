"""
Test for coi kill --all --force - kill all containers without confirmation.

Tests that:
1. Launch multiple containers
2. Kill all with --all --force
3. Verify all are deleted
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_kill_all_with_force(coi_binary, cleanup_containers, workspace_dir):
    """
    Test killing all containers with --all --force.

    Flow:
    1. Launch 2 containers
    2. Kill all with --all --force
    3. Verify all are deleted
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

    # === Phase 2: Kill all with --all --force ===

    result = subprocess.run(
        [coi_binary, "kill", "--all", "--force"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Kill --all should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    # Should show found containers and killed count
    assert "Found" in combined_output or "container" in combined_output.lower(), (
        f"Should show container info. Got:\n{combined_output}"
    )

    # === Phase 3: Verify all deleted ===

    for container_name in [container1, container2]:
        result = subprocess.run(
            [coi_binary, "container", "exists", container_name],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert result.returncode != 0, (
            f"Container {container_name} should not exist after kill --all"
        )
