"""
Test for coi tmux list - when no tmux sessions exist.

Tests that:
1. Ensure no containers exist
2. Run tmux list
3. Verify appropriate message shown
"""

import subprocess


def test_tmux_list_no_sessions(coi_binary, cleanup_containers):
    """
    Test listing tmux sessions when none exist.

    Flow:
    1. Run coi tmux list with no containers
    2. Verify appropriate message shown
    """

    # === Phase 1: List tmux sessions (should be none) ===

    result = subprocess.run(
        [coi_binary, "tmux", "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed even with no sessions
    assert result.returncode == 0, f"Tmux list should succeed. stderr: {result.stderr}"

    # Should show "No active sessions"
    assert "No active sessions" in result.stdout, (
        f"Should show no sessions message. Got:\n{result.stdout}"
    )
