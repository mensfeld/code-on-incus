"""
Test for coi run - with --slot flag.

Tests that:
1. Run command with --slot flag
2. Verify command executes successfully
"""

import subprocess


def test_run_with_slot(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running command with specific slot.

    Flow:
    1. Run coi run --slot 5 "echo hello"
    2. Verify command succeeds
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--slot", "5", "echo", "slot-test-123"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run with slot should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "slot-test-123" in combined_output, (
        f"Output should contain echo text. Got:\n{combined_output}"
    )
