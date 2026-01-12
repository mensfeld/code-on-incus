"""
Test for coi image exists - check existing image.

Tests that:
1. Check for coi image (should exist after build)
2. Verify exit code is 0
"""

import subprocess


def test_exists_coi_image(coi_binary, cleanup_containers):
    """
    Test checking if the coi image exists.

    Flow:
    1. Run coi image exists coi
    2. Verify exit code is 0 (image exists)

    Note: This test assumes the coi image has been built.
    """
    # === Phase 1: Check if coi image exists ===

    result = subprocess.run(
        [coi_binary, "image", "exists", "coi"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify success ===

    assert result.returncode == 0, f"coi image should exist. stderr: {result.stderr}"
