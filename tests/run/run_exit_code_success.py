"""
Test for coi run - exit code 0 for successful command.

Tests that:
1. Run a successful command
2. Verify exit code is 0
"""

import subprocess


def test_run_exit_code_success(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that successful command returns exit code 0.

    Flow:
    1. Run coi run "true" (always succeeds)
    2. Verify exit code is 0
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "true"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"'true' command should exit with 0. stderr: {result.stderr}"
