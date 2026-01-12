"""
Test for coi run - no command provided.

Tests that:
1. Run coi run without a command
2. Verify it fails with usage error
"""

import subprocess


def test_run_no_command(coi_binary, cleanup_containers):
    """
    Test that coi run without command shows error.

    Flow:
    1. Run coi run (no command)
    2. Verify it fails with usage message
    """
    result = subprocess.run(
        [coi_binary, "run"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, f"Run without command should fail. stdout: {result.stdout}"

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "usage" in combined_output or "required" in combined_output or "argument" in combined_output
    ), f"Should show usage error. Got:\n{result.stdout + result.stderr}"
