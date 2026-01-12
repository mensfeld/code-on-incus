"""
Test for coi file pull - pull from nonexistent container.

Tests that:
1. Try to pull from a container that doesn't exist
2. Verify appropriate error message
"""

import os
import subprocess

from support.helpers import calculate_container_name


def test_pull_nonexistent_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that pulling from a nonexistent container fails gracefully.

    Flow:
    1. Try to pull from nonexistent container
    2. Verify error message
    """
    container_name = calculate_container_name(workspace_dir, 1)
    fake_container = f"{container_name}-does-not-exist"
    local_file = os.path.join(workspace_dir, "should-not-exist.txt")

    # === Phase 1: Try to pull from nonexistent container ===

    result = subprocess.run(
        [coi_binary, "file", "pull", f"{fake_container}:/tmp/test.txt", local_file],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure ===

    assert result.returncode != 0, (
        f"Pull from nonexistent container should fail. stdout: {result.stdout}"
    )

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "failed" in combined_output or "not found" in combined_output or "error" in combined_output
    ), f"Should show error message. Got:\n{result.stdout + result.stderr}"

    # Verify file was not created
    assert not os.path.exists(local_file), "Local file should not be created on failure"
