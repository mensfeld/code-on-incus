"""
Test for coi run - execute command with multiple arguments.

Tests that:
1. Run a command with multiple arguments
2. Verify all arguments are passed correctly
"""

import subprocess


def test_run_command_with_args(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running a command with multiple arguments.

    Flow:
    1. Run coi run with multiple args
    2. Verify output shows all args were received
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "echo", "arg1", "arg2", "arg3"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "arg1" in combined_output, f"Output should contain arg1. Got:\n{combined_output}"
    assert "arg2" in combined_output, f"Output should contain arg2. Got:\n{combined_output}"
    assert "arg3" in combined_output, f"Output should contain arg3. Got:\n{combined_output}"
