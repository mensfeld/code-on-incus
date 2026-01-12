"""
Test for coi run - execute a simple command.

Tests that:
1. Run a simple echo command
2. Verify output contains expected text
3. Verify exit code is 0
"""

import subprocess


def test_run_simple_command(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running a simple echo command.

    Flow:
    1. Run coi run "echo hello-test-xyz"
    2. Verify output contains the text
    3. Verify success exit code
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "echo", "hello-test-xyz-123"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    # Output should contain our text
    combined_output = result.stdout + result.stderr
    assert "hello-test-xyz-123" in combined_output, (
        f"Output should contain echo text. Got:\n{combined_output}"
    )
