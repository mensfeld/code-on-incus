"""
Test for coi file pull - invalid source format.

Tests that:
1. Try to pull with source missing colon separator
2. Verify appropriate error message
"""

import os
import subprocess


def test_pull_invalid_source_format(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that pull with invalid source format fails gracefully.

    Flow:
    1. Try to pull with invalid format (no colon)
    2. Verify error message about format
    """
    local_file = os.path.join(workspace_dir, "should-not-exist.txt")

    # === Phase 1: Try to pull with invalid format ===

    result = subprocess.run(
        [coi_binary, "file", "pull", "container-no-path", local_file],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure with format error ===

    assert result.returncode != 0, f"Pull with invalid format should fail. stdout: {result.stdout}"

    combined_output = (result.stdout + result.stderr).lower()
    assert "container:path" in combined_output or "format" in combined_output, (
        f"Should mention correct format. Got:\n{result.stdout + result.stderr}"
    )
