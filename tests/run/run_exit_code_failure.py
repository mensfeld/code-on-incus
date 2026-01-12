"""
Test for coi run - non-zero exit code for failing command.

Tests that:
1. Run a failing command
2. Verify exit code is non-zero
"""

import subprocess


def test_run_exit_code_failure(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that failing command returns non-zero exit code.

    Flow:
    1. Run coi run "false" (always fails with exit 1)
    2. Verify exit code is non-zero
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "false"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode != 0, (
        f"'false' command should exit with non-zero. stdout: {result.stdout}"
    )
