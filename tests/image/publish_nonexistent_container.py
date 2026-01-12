"""
Test for coi image publish - nonexistent container.

Tests that:
1. Try to publish a container that doesn't exist
2. Verify it fails with appropriate error
"""

import subprocess


def test_publish_nonexistent_container(coi_binary, cleanup_containers):
    """
    Test publishing a nonexistent container fails gracefully.

    Flow:
    1. Run coi image publish nonexistent-container test-image
    2. Verify it fails with error message
    """
    # === Phase 1: Try to publish nonexistent container ===

    result = subprocess.run(
        [coi_binary, "image", "publish", "nonexistent-container-xyz-123", "test-image-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure ===

    assert result.returncode != 0, (
        f"Publishing nonexistent container should fail. stdout: {result.stdout}"
    )

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "failed" in combined_output or "not found" in combined_output or "error" in combined_output
    ), f"Should show error message. Got:\n{result.stdout + result.stderr}"
