"""
Test for coi tmux capture - error when missing required arguments.

Tests that:
1. Try to use tmux capture with no arguments
2. Verify error message about usage
"""

import subprocess


def test_tmux_capture_missing_args(coi_binary, cleanup_containers):
    """
    Test tmux capture fails when session name is missing.

    Flow:
    1. Try coi tmux capture with no args
    2. Verify usage/error shown
    """

    # === Phase 1: No arguments ===

    result = subprocess.run(
        [coi_binary, "tmux", "capture"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail
    assert result.returncode != 0, "Tmux capture should fail with no arguments"

    combined_output = result.stdout + result.stderr

    # Should show usage or error about arguments
    assert (
        "usage:" in combined_output.lower()
        or "requires" in combined_output.lower()
        or "error" in combined_output.lower()
        or "accepts" in combined_output.lower()
    ), f"Should show usage/error. Got:\n{combined_output}"
