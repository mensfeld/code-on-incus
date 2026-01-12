"""
Test for coi run - with single environment variable.

Tests that:
1. Run command with -e flag to set env var
2. Verify env var is available in container
"""

import subprocess


def test_run_with_env(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running command with environment variables.

    Flow:
    1. Run coi run -e MY_VAR=test123 env
    2. Verify MY_VAR appears in output
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "-e",
            "MY_TEST_VAR=test-value-xyz",
            "--",
            "sh",
            "-c",
            "echo $MY_TEST_VAR",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "test-value-xyz" in combined_output, (
        f"Output should contain env var value. Got:\n{combined_output}"
    )
