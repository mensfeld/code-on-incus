"""
Test for coi run - specific exit code propagation.

Tests that:
1. Run a command with specific exit code
2. Verify exit code is propagated correctly
"""

import subprocess


def test_run_exit_code_specific(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that specific exit codes are propagated.

    Flow:
    1. Run coi run "exit 42"
    2. Verify exit code is 42
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "sh", "-c", "exit 42"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 42, (
        f"Should propagate exit code 42. Got: {result.returncode}. stderr: {result.stderr}"
    )
