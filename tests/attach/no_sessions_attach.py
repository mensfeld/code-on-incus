"""
Test for coi attach - no sessions running.

Tests that:
1. Run coi attach when no containers are running
2. Verify it shows empty session list with usage hint
"""

import subprocess


def test_attach_no_sessions(coi_binary, cleanup_containers):
    """
    Test that coi attach with no running containers shows appropriate message.

    Flow:
    1. Ensure no containers are running
    2. Run coi attach
    3. Verify output shows usage hint (empty list)
    """
    # Run coi attach with no containers running
    result = subprocess.run(
        [coi_binary, "attach"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed (exit 0) and show usage hint
    assert result.returncode == 0, \
        f"coi attach should succeed. stderr: {result.stderr}"

    # When no sessions, shows header and usage hint
    output = result.stdout
    assert "Active Claude sessions" in output or "No active" in output, \
        f"Should show session info. Got:\n{output}"

    # Should show usage hint since no containers to auto-attach
    assert "coi attach" in output, \
        f"Should show usage hint. Got:\n{output}"
