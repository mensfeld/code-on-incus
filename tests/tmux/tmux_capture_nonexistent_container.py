"""
Test for coi tmux capture - error when container doesn't exist.

Tests that:
1. Try to capture from nonexistent container
2. Verify error message
"""

import subprocess


def test_tmux_capture_nonexistent_container(coi_binary, cleanup_containers):
    """
    Test tmux capture fails gracefully when container doesn't exist.

    Flow:
    1. Try to use coi tmux capture on nonexistent container
    2. Verify error message
    """
    fake_container = "coi-nonexistent-tmux-test-88888"

    # === Phase 1: Try to capture from nonexistent container ===

    result = subprocess.run(
        [coi_binary, "tmux", "capture", fake_container],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail
    assert result.returncode != 0, "Tmux capture should fail for nonexistent container"

    combined_output = result.stdout + result.stderr

    # Should show error about container status or not found
    assert (
        "not running" in combined_output.lower()
        or "failed to check" in combined_output.lower()
        or "error" in combined_output.lower()
    ), f"Should indicate error. Got:\n{combined_output}"
