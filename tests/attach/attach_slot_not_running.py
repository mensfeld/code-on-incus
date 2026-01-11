"""
Test for coi attach --slot - slot not running.

Tests that:
1. Run coi attach --slot=5 when no container is running on that slot
2. Verify it shows an error message
"""

import subprocess


def test_attach_slot_not_running(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi attach --slot with no container shows error.

    Flow:
    1. Run coi attach --slot=5 (no container running)
    2. Verify it returns error about container not found
    """
    result = subprocess.run(
        [coi_binary, "attach", "--slot=5", f"--workspace={workspace_dir}"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail
    assert result.returncode != 0, \
        f"coi attach --slot should fail when no container running. stdout: {result.stdout}"

    assert "not found" in result.stderr.lower() or "not running" in result.stderr.lower(), \
        f"Should show 'not found' or 'not running' error. Got:\n{result.stderr}"
