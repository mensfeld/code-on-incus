"""
Test for coi tmux send - error when container doesn't exist.

Tests that:
1. Try to send command to nonexistent container
2. Verify error message
"""

import subprocess


def test_tmux_send_nonexistent_container(coi_binary, cleanup_containers):
    """
    Test tmux send fails gracefully when container doesn't exist.

    Flow:
    1. Try to use coi tmux send on nonexistent container
    2. Verify error message
    """
    fake_container = "coi-nonexistent-tmux-test-99999"

    # === Phase 1: Try to send command to nonexistent container ===

    result = subprocess.run(
        [coi_binary, "tmux", "send", fake_container, "echo test"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail
    assert result.returncode != 0, "Tmux send should fail for nonexistent container"

    combined_output = result.stdout + result.stderr

    # Should show error about container status or not found
    # The actual error depends on whether it checks existence first or running state
    assert (
        "not running" in combined_output.lower()
        or "failed to check" in combined_output.lower()
        or "error" in combined_output.lower()
    ), f"Should indicate error. Got:\n{combined_output}"
