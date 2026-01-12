"""
Test for coi container stop - fails for nonexistent container.

Tests that:
1. Try to stop a container that doesn't exist
2. Verify command fails with appropriate error
"""

import subprocess


def test_stop_nonexistent(coi_binary, cleanup_containers):
    """
    Test that stopping nonexistent container fails.

    Flow:
    1. Try to stop a nonexistent container
    2. Verify command fails
    3. Verify error message is appropriate
    """
    nonexistent_name = "nonexistent-container-12345"

    # === Phase 1: Try to stop nonexistent container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", nonexistent_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure ===

    assert result.returncode != 0, "Stopping nonexistent container should fail"

    combined_output = result.stdout + result.stderr
    has_error = (
        "not found" in combined_output.lower()
        or "does not exist" in combined_output.lower()
        or "error" in combined_output.lower()
        or nonexistent_name in combined_output
    )

    assert has_error, f"Should indicate container not found. Got:\n{combined_output}"
