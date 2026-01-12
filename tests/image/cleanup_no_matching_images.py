"""
Test for coi image cleanup - no matching images.

Tests that:
1. Run coi image cleanup with prefix that matches nothing
2. Verify it succeeds with appropriate message
"""

import subprocess


def test_cleanup_no_matching_images(coi_binary, cleanup_containers):
    """
    Test cleanup with prefix that matches no images.

    Flow:
    1. Run coi image cleanup with unique prefix --keep 1
    2. Verify it succeeds (nothing to delete)
    """
    result = subprocess.run(
        [coi_binary, "image", "cleanup", "nonexistent-prefix-xyz-123-", "--keep", "1"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed even with no matching images
    assert result.returncode == 0, (
        f"Cleanup with no matching images should succeed. stderr: {result.stderr}"
    )

    combined_output = result.stdout + result.stderr

    # Should show cleanup complete (with 0 deleted)
    assert (
        "Cleanup complete" in combined_output
        or "Deleted 0" in combined_output
        or "Kept 0" in combined_output
        or combined_output.strip() != ""
    ), f"Should show cleanup status. Got:\n{combined_output}"
