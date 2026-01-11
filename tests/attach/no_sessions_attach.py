"""
Test for coi attach - no sessions running.

Tests that:
1. Run coi attach when no containers are running
2. Verify it shows "No active Claude sessions"
"""

import subprocess


def test_attach_no_sessions(coi_binary, cleanup_containers):
    """
    Test that coi attach with no running containers shows appropriate message.

    Flow:
    1. Ensure no containers are running
    2. Run coi attach
    3. Verify output shows "No active Claude sessions"
    """
    # Run coi attach with no containers running
    result = subprocess.run(
        [coi_binary, "attach"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed (exit 0) but show no sessions message
    assert result.returncode == 0, \
        f"coi attach should succeed. stderr: {result.stderr}"

    assert "No active Claude sessions" in result.stdout, \
        f"Should show 'No active Claude sessions'. Got:\n{result.stdout}"
