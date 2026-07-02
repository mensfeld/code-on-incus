"""
Test for coi run - no command and no boot script.

Tests that:
1. Run coi run without a command in a workspace with no coi-boot.sh
2. Verify it fails with a clear error pointing at the boot-script convention
"""

import subprocess


def test_run_no_command(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi run without a command and without coi-boot.sh shows an error.

    Flow:
    1. Run coi run (no command) in a workspace without coi-boot.sh
    2. Verify it fails and the error explains both options (script or command)
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, f"Run without command should fail. stdout: {result.stdout}"

    combined_output = result.stdout + result.stderr
    assert "coi-boot.sh" in combined_output, (
        f"Error should mention the coi-boot.sh convention. Got:\n{combined_output}"
    )
    assert "no command given" in combined_output, (
        f"Error should say no command was given. Got:\n{combined_output}"
    )
