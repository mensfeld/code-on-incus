"""
Test for coi image cleanup - missing --keep flag.

Tests that:
1. Run coi image cleanup without --keep
2. Verify it fails with error about required flag
"""

import subprocess


def test_cleanup_missing_keep_flag(coi_binary, cleanup_containers):
    """
    Test that cleanup without --keep flag fails.

    Flow:
    1. Run coi image cleanup prefix (no --keep)
    2. Verify it fails with appropriate error
    """
    result = subprocess.run(
        [coi_binary, "image", "cleanup", "test-prefix-"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, f"Missing --keep should fail. stdout: {result.stdout}"

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "keep" in combined_output or "required" in combined_output or "flag" in combined_output
    ), f"Should indicate --keep is required. Got:\n{result.stdout + result.stderr}"
