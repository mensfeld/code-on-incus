"""
Test for coi container exec - verify --tty and --capture are mutually exclusive.

Tests that:
1. Attempting to use both --tty and --capture flags together should fail
2. Error message should be clear
"""

import subprocess

from support.helpers import (
    calculate_container_name,
)


def test_exec_tty_capture_conflict(coi_binary, workspace_dir):
    """
    Test that --tty and --capture flags cannot be used together.

    Flow:
    1. Try to execute command with both --tty and --capture
    2. Verify command fails with exit code 2 (usage error)
    3. Verify error message mentions the conflict
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Test: Try to use both --tty and --capture ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--tty",
            "--capture",
            "--",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail with exit code 2 (usage error)
    assert result.returncode == 2, (
        f"Using both --tty and --capture should fail with exit code 2. "
        f"Got exit code: {result.returncode}"
    )

    # Error message should mention the conflict
    combined_output = result.stdout + result.stderr
    assert "mutually exclusive" in combined_output.lower(), (
        f"Error message should mention flags are mutually exclusive. Got output:\n{combined_output}"
    )
