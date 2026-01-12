"""
Test for coi run - with --persistent flag.

Tests that:
1. Run with --persistent flag
2. Container is stopped but not deleted
3. Second run reuses the container
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_run_with_persistent(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running with --persistent flag.

    Flow:
    1. Run coi run --persistent --slot N
    2. Verify command succeeds
    3. Verify container still exists (stopped)
    4. Run again and verify it reuses container
    5. Cleanup
    """
    slot = 8
    container_name = calculate_container_name(workspace_dir, slot)

    # === Phase 1: First run with persistent ===

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--persistent",
            "--slot",
            str(slot),
            "echo",
            "first-run-persistent",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"First run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "first-run-persistent" in combined_output, (
        f"Output should contain echo text. Got:\n{combined_output}"
    )

    time.sleep(2)

    # === Phase 2: Verify container exists (stopped) ===

    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Container should still exist after persistent run"

    # === Phase 3: Second run reuses container ===

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--persistent",
            "--slot",
            str(slot),
            "echo",
            "second-run-reused",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Second run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "second-run-reused" in combined_output, (
        f"Output should contain echo text. Got:\n{combined_output}"
    )

    # Should show "Restarting existing" message
    assert "existing" in combined_output.lower() or "restart" in combined_output.lower(), (
        f"Should indicate container reuse. Got:\n{combined_output}"
    )

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
