"""
Test for coi file push - push to nonexistent container.

Tests that:
1. Create a local file
2. Try to push to a container that doesn't exist
3. Verify appropriate error message
"""

import os
import subprocess

from support.helpers import calculate_container_name


def test_push_nonexistent_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that pushing to a nonexistent container fails gracefully.

    Flow:
    1. Create a test file locally
    2. Try to push to nonexistent container
    3. Verify error message
    """
    container_name = calculate_container_name(workspace_dir, 1)
    fake_container = f"{container_name}-does-not-exist"

    # === Phase 1: Create local test file ===

    local_file = os.path.join(workspace_dir, "test-push.txt")
    with open(local_file, "w") as f:
        f.write("test content")

    # === Phase 2: Try to push to nonexistent container ===

    result = subprocess.run(
        [coi_binary, "file", "push", local_file, f"{fake_container}:/tmp/test.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 3: Verify failure ===

    assert result.returncode != 0, (
        f"Push to nonexistent container should fail. stdout: {result.stdout}"
    )

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "failed" in combined_output or "not found" in combined_output or "error" in combined_output
    ), f"Should show error message. Got:\n{result.stdout + result.stderr}"
