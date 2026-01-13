"""
Test for coi tmux send - error when missing required arguments.

Tests that:
1. Try to use tmux send with missing arguments
2. Verify error message about usage
"""

import subprocess


def test_tmux_send_missing_args(coi_binary, cleanup_containers):
    """
    Test tmux send fails when arguments are missing.

    Flow:
    1. Try coi tmux send with no args
    2. Try coi tmux send with only session name (no command)
    3. Verify both show usage/error
    """

    # === Phase 1: No arguments ===

    result = subprocess.run(
        [coi_binary, "tmux", "send"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail
    assert result.returncode != 0, "Tmux send should fail with no arguments"

    combined_output = result.stdout + result.stderr

    # Should show usage or error about arguments
    assert (
        "usage:" in combined_output.lower()
        or "requires" in combined_output.lower()
        or "error" in combined_output.lower()
        or "accepts" in combined_output.lower()
    ), f"Should show usage/error. Got:\n{combined_output}"

    # === Phase 2: Only session name (missing command) ===

    result = subprocess.run(
        [coi_binary, "tmux", "send", "some-container"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail
    assert result.returncode != 0, "Tmux send should fail with only one argument"

    combined_output = result.stdout + result.stderr

    # Should show usage or error about arguments
    assert (
        "usage:" in combined_output.lower()
        or "requires" in combined_output.lower()
        or "error" in combined_output.lower()
        or "accepts" in combined_output.lower()
    ), f"Should show usage/error. Got:\n{combined_output}"
