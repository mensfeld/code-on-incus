"""
Test for coi run - multi-statement shell command.

Tests that:
1. Run command with multiple statements
2. Verify all statements execute
"""

import subprocess


def test_run_multiline_command(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running multi-statement command.

    Flow:
    1. Run coi run with multiple statements
    2. Verify all statements execute
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
            "echo first; echo second; echo third",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, (
        f"Multi-statement command should succeed. stderr: {result.stderr}"
    )

    combined_output = result.stdout + result.stderr
    assert "first" in combined_output, f"Output should contain 'first'. Got:\n{combined_output}"
    assert "second" in combined_output, f"Output should contain 'second'. Got:\n{combined_output}"
    assert "third" in combined_output, f"Output should contain 'third'. Got:\n{combined_output}"
