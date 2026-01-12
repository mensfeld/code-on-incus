"""
Test for coi run - with nonexistent image.

Tests that:
1. Run with --image pointing to nonexistent image
2. Verify it fails with appropriate error
"""

import subprocess


def test_run_nonexistent_image(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that run with nonexistent image fails gracefully.

    Flow:
    1. Run coi run --image nonexistent-xyz
    2. Verify it fails with image not found error
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--image",
            "nonexistent-image-xyz-123",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode != 0, (
        f"Run with nonexistent image should fail. stdout: {result.stdout}"
    )

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "not found" in combined_output
        or "not exist" in combined_output
        or "build" in combined_output
    ), f"Should show image not found error. Got:\n{result.stdout + result.stderr}"
