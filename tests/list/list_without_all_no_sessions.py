"""
Test for coi list - without --all doesn't show sessions.

Tests that:
1. Run coi list (without --all)
2. Verify it does NOT show Saved Sessions section
"""

import subprocess


def test_list_without_all_no_sessions(coi_binary, cleanup_containers):
    """
    Test that coi list without --all doesn't show Saved Sessions.

    Flow:
    1. Run coi list (no --all flag)
    2. Verify Saved Sessions section is NOT shown
    """
    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should show Active Containers section
    assert "Active Containers:" in output, f"Should show Active Containers section. Got:\n{output}"

    # Should NOT show Saved Sessions section (requires --all)
    assert "Saved Sessions:" not in output, (
        f"Should NOT show Saved Sessions without --all. Got:\n{output}"
    )
