"""
Test for coi run - executes as code user.

Tests that:
1. Run whoami command
2. Verify it runs as 'code' user
"""

import subprocess


def test_run_as_code_user(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that commands run as the code user.

    Flow:
    1. Run coi run whoami
    2. Verify output is 'code'
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "whoami"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "code" in combined_output, f"Should run as 'code' user. Got:\n{combined_output}"
