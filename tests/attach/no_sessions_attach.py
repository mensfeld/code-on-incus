"""
Test for coi attach - shows session list.

Tests that:
1. Run coi attach (no container name)
2. Verify it shows session list or usage hint
"""

import subprocess


def test_attach_shows_sessions(coi_binary, cleanup_containers):
    """
    Test that coi attach without arguments shows session list.

    Flow:
    1. Run coi attach
    2. Verify output shows session info or usage hint
    """
    # Run coi attach without container name
    result = subprocess.run(
        [coi_binary, "attach"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed (exit 0) - shows list or usage
    assert result.returncode == 0, f"coi attach should succeed. stderr: {result.stderr}"

    # Should show session info (either active sessions or "no active" message)
    combined_output = result.stdout + result.stderr
    assert (
        "Active Claude sessions" in combined_output
        or "No active" in combined_output
        or "coi attach" in combined_output
    ), f"Should show session info or usage hint. Got:\n{combined_output}"
