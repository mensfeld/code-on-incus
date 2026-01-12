"""
Test for coi info - no sessions exist.

Tests that:
1. Run coi info without arguments when no sessions exist
2. Verify it fails with appropriate error message

Note: This test requires a clean state with no saved sessions.
      It may be skipped if sessions from other tests exist.
"""

import subprocess


def test_info_no_sessions(coi_binary, cleanup_containers):
    """
    Test that coi info without args fails when no sessions exist.

    Flow:
    1. Run coi info (no args)
    2. If no sessions exist, should fail with "no sessions found"
    3. If sessions exist (from other tests), behavior is acceptable

    Note: This test is best-effort - other test sessions may exist.
    """
    # === Phase 1: Run info without arguments ===

    result = subprocess.run(
        [coi_binary, "info"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined_output = result.stdout + result.stderr

    # === Phase 2: Check result ===

    if result.returncode != 0:
        # No sessions - expected when clean
        assert "no session" in combined_output.lower() or "not found" in combined_output.lower(), (
            f"Should show 'no sessions' error. Got:\n{combined_output}"
        )
    else:
        # Sessions exist from other tests - info should show session data
        assert "Session" in combined_output or "session" in combined_output.lower(), (
            f"Should show session info. Got:\n{combined_output}"
        )
