"""
Test for coi shutdown --all when no containers exist.

Tests that:
1. Ensure no coi containers exist
2. Run shutdown --all
3. Verify appropriate message
"""

import subprocess


def test_shutdown_all_no_containers(coi_binary, cleanup_containers):
    """
    Test shutdown --all when no containers exist.

    Flow:
    1. First clean up any existing containers
    2. Run coi shutdown --all --force again
    3. Verify it shows "No containers to shutdown"
    """
    # First, clean up any containers that may exist from other tests
    subprocess.run(
        [coi_binary, "shutdown", "--all", "--force"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Now run again - should show "No containers to shutdown"
    result = subprocess.run(
        [coi_binary, "shutdown", "--all", "--force"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed (not an error, just nothing to do)
    assert result.returncode == 0, (
        f"Shutdown --all with no containers should succeed. stderr: {result.stderr}"
    )

    combined_output = (result.stdout + result.stderr).lower()
    assert "no containers" in combined_output, (
        f"Should show 'No containers to shutdown'. Got:\n{result.stdout + result.stderr}"
    )
