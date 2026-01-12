"""
Test for coi run - with multiple environment variables.

Tests that:
1. Run command with multiple -e flags
2. Verify all env vars are set
"""

import subprocess


def test_run_with_multiple_env(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running command with multiple environment variables.

    Flow:
    1. Run coi run with multiple -e flags
    2. Verify all env vars are set
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "-e",
            "VAR1=value1",
            "-e",
            "VAR2=value2",
            "--",
            "sh",
            "-c",
            "echo $VAR1 $VAR2",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "value1" in combined_output, f"Output should contain VAR1 value. Got:\n{combined_output}"
    assert "value2" in combined_output, f"Output should contain VAR2 value. Got:\n{combined_output}"
