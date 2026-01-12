"""
Test for coi run - shell command with pipes.

Tests that:
1. Run command with pipes
2. Verify output is correct
"""

import subprocess


def test_run_pipe_command(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running command with pipes.

    Flow:
    1. Run coi run with a pipe command
    2. Verify output is correct
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo 'hello world' | grep hello",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Pipe command should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "hello" in combined_output, f"Output should contain 'hello'. Got:\n{combined_output}"
