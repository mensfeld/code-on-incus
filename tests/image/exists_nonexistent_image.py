"""
Test for coi image exists - check nonexistent image.

Tests that:
1. Check for an image that doesn't exist
2. Verify exit code is non-zero
"""

import subprocess


def test_exists_nonexistent_image(coi_binary, cleanup_containers):
    """
    Test checking if a nonexistent image exists.

    Flow:
    1. Run coi image exists nonexistent-image-xyz-123
    2. Verify exit code is non-zero (image doesn't exist)
    """
    # === Phase 1: Check nonexistent image ===

    result = subprocess.run(
        [coi_binary, "image", "exists", "nonexistent-image-xyz-123-abc"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure ===

    assert result.returncode != 0, f"Nonexistent image check should fail. stdout: {result.stdout}"
