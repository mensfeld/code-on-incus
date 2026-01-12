"""
Test for coi container launch - fails with nonexistent image.

Tests that:
1. Try to launch with a nonexistent image
2. Verify command fails with appropriate error
"""

import subprocess


def test_launch_nonexistent_image(coi_binary, cleanup_containers):
    """
    Test that launching with nonexistent image fails.

    Flow:
    1. Try to launch container with nonexistent image
    2. Verify command fails
    3. Verify error message mentions the image
    """
    # === Phase 1: Try to launch with nonexistent image ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "nonexistent-image-12345", "test-container"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure ===

    assert result.returncode != 0, "Launch with nonexistent image should fail"

    combined_output = result.stdout + result.stderr
    assert (
        "nonexistent" in combined_output.lower()
        or "not found" in combined_output.lower()
        or "error" in combined_output.lower()
    ), f"Error should mention the image issue. Got:\n{combined_output}"
