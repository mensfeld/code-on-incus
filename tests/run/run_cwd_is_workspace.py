"""
Test for coi run - current working directory is /workspace.

Tests that:
1. Run pwd command
2. Verify CWD is /workspace
"""

import subprocess


def test_run_cwd_is_workspace(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that current working directory is /workspace.

    Flow:
    1. Run coi run pwd
    2. Verify output shows /workspace
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "pwd"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "/workspace" in combined_output, f"CWD should be /workspace. Got:\n{combined_output}"
