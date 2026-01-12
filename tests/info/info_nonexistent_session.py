"""
Test for coi info - nonexistent session ID.

Tests that:
1. Run coi info with a session ID that doesn't exist
2. Verify it fails with appropriate error message
"""

import subprocess


def test_info_nonexistent_session(coi_binary, cleanup_containers):
    """
    Test that coi info with nonexistent session ID fails gracefully.

    Flow:
    1. Run coi info nonexistent-session-xyz-123
    2. Verify it fails with "session not found" error
    """
    # === Phase 1: Run info with nonexistent session ===

    result = subprocess.run(
        [coi_binary, "info", "nonexistent-session-xyz-123-abc"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure ===

    assert result.returncode != 0, (
        f"Info for nonexistent session should fail. stdout: {result.stdout}"
    )

    combined_output = (result.stdout + result.stderr).lower()
    assert "not found" in combined_output or "no session" in combined_output, (
        f"Should show 'not found' error. Got:\n{result.stdout + result.stderr}"
    )
