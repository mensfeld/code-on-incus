"""
Test for coi file push - invalid destination format.

Tests that:
1. Create a local file
2. Try to push with destination missing colon separator
3. Verify appropriate error message
"""

import os
import subprocess


def test_push_invalid_destination_format(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that push with invalid destination format fails gracefully.

    Flow:
    1. Create a test file locally
    2. Try to push with invalid format (no colon)
    3. Verify error message about format
    """
    # === Phase 1: Create local test file ===

    local_file = os.path.join(workspace_dir, "test-push.txt")
    with open(local_file, "w") as f:
        f.write("test content")

    # === Phase 2: Try to push with invalid format ===

    result = subprocess.run(
        [coi_binary, "file", "push", local_file, "container-no-path"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 3: Verify failure with format error ===

    assert result.returncode != 0, f"Push with invalid format should fail. stdout: {result.stdout}"

    combined_output = (result.stdout + result.stderr).lower()
    assert "container:path" in combined_output or "format" in combined_output, (
        f"Should mention correct format. Got:\n{result.stdout + result.stderr}"
    )
