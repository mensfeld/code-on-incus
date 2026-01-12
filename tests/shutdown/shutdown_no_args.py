"""
Test for coi shutdown - no arguments provided.

Tests that:
1. Run shutdown without arguments
2. Verify it fails with appropriate error
"""

import subprocess


def test_shutdown_no_args(coi_binary, cleanup_containers):
    """
    Test that shutdown without arguments shows error.

    Flow:
    1. Run coi shutdown (no args, no --all)
    2. Verify it fails with usage message
    """
    result = subprocess.run(
        [coi_binary, "shutdown"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, f"Shutdown without args should fail. stdout: {result.stdout}"

    combined_output = (result.stdout + result.stderr).lower()
    assert "no container" in combined_output or "provided" in combined_output, (
        f"Should show 'no container names provided' error. Got:\n{result.stdout + result.stderr}"
    )
