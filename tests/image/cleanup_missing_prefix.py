"""
Test for coi image cleanup - missing prefix argument.

Tests that:
1. Run coi image cleanup without prefix
2. Verify it fails with usage error
"""

import subprocess


def test_cleanup_missing_prefix(coi_binary, cleanup_containers):
    """
    Test that cleanup without prefix argument fails.

    Flow:
    1. Run coi image cleanup --keep 1 (no prefix)
    2. Verify it fails with usage message
    """
    result = subprocess.run(
        [coi_binary, "image", "cleanup", "--keep", "1"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, f"Missing prefix should fail. stdout: {result.stdout}"

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "usage" in combined_output or "required" in combined_output or "argument" in combined_output
    ), f"Should show usage error. Got:\n{result.stdout + result.stderr}"
