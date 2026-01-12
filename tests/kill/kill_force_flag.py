"""
Test for coi kill --force - skip confirmation for single container.

Tests that:
1. Launch a container
2. Kill with --force (no confirmation needed)
3. Verify killed
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_kill_with_force_flag(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that --force skips confirmation.

    Flow:
    1. Launch a container
    2. Kill with --force
    3. Verify it's deleted
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

    # === Phase 2: Kill with --force ===

    result = subprocess.run(
        [coi_binary, "kill", "--force", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Kill --force should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Killed" in combined_output or "killed" in combined_output.lower(), (
        f"Should show killed confirmation. Got:\n{combined_output}"
    )

    # === Phase 3: Verify deleted ===

    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Container should not exist after kill"
