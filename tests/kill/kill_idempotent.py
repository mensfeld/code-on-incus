"""
Test for coi kill - killing same container twice.

Tests that:
1. Launch a container
2. Kill it
3. Try to kill again
4. Verify it handles gracefully
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_kill_idempotent(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that killing a container twice is handled gracefully.

    Flow:
    1. Launch a container
    2. Kill it
    3. Try to kill again
    4. Verify second kill handles the situation
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

    # === Phase 2: First kill ===

    result = subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"First kill should succeed. stderr: {result.stderr}"

    # === Phase 3: Second kill (container no longer exists) ===

    result = subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    # Second kill may fail or show warning - both are acceptable
    combined_output = result.stdout + result.stderr

    if result.returncode != 0:
        # Failed is acceptable for already-killed container
        assert (
            "failed" in combined_output.lower()
            or "warning" in combined_output.lower()
            or "not exist" in combined_output.lower()
            or "No containers" in combined_output
        ), f"Should show appropriate message. Got:\n{combined_output}"
    # Success with warning is also acceptable
