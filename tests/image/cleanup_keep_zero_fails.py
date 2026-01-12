"""
Test for coi image cleanup - keep=0 should fail.

Tests that:
1. Run coi image cleanup with --keep 0
2. Verify it fails with error message
"""

import subprocess


def test_cleanup_keep_zero_fails(coi_binary, cleanup_containers):
    """
    Test that cleanup with --keep 0 fails.

    Flow:
    1. Run coi image cleanup prefix --keep 0
    2. Verify it fails with appropriate error
    """
    result = subprocess.run(
        [coi_binary, "image", "cleanup", "test-prefix-", "--keep", "0"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, f"--keep 0 should fail. stdout: {result.stdout}"

    combined_output = result.stdout + result.stderr
    assert (
        "--keep" in combined_output
        or "> 0" in combined_output
        or "must be" in combined_output.lower()
    ), f"Should indicate --keep must be > 0. Got:\n{combined_output}"
