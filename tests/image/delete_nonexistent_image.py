"""
Test for coi image delete - delete nonexistent image.

Tests that:
1. Try to delete an image that doesn't exist
2. Verify it fails with appropriate error
"""

import subprocess


def test_delete_nonexistent_image(coi_binary, cleanup_containers):
    """
    Test deleting a nonexistent image fails gracefully.

    Flow:
    1. Run coi image delete nonexistent-image
    2. Verify it fails with error message
    """
    # === Phase 1: Try to delete nonexistent image ===

    result = subprocess.run(
        [coi_binary, "image", "delete", "nonexistent-image-xyz-123"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure ===

    assert result.returncode != 0, (
        f"Deleting nonexistent image should fail. stdout: {result.stdout}"
    )

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "failed" in combined_output or "not found" in combined_output or "error" in combined_output
    ), f"Should show error message. Got:\n{result.stdout + result.stderr}"
