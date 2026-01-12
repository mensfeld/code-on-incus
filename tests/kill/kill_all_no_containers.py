"""
Test for coi kill --all - when no containers exist.

Tests that:
1. Run coi kill --all when no containers exist
2. Verify it handles gracefully
"""

import subprocess


def test_kill_all_no_containers(coi_binary, cleanup_containers):
    """
    Test coi kill --all when no containers exist.

    Flow:
    1. Run coi kill --all --force
    2. Verify it succeeds with "no containers" message

    Note: This test may find containers from other tests if run in parallel.
    """
    result = subprocess.run(
        [coi_binary, "kill", "--all", "--force"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    # Should succeed (no error even with no containers)
    assert result.returncode == 0, f"Kill --all should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr

    # Either "no containers to kill" or actually killed some (from other tests)
    assert (
        "No containers to kill" in combined_output
        or "Killed" in combined_output
        or "container" in combined_output.lower()
    ), f"Should show status message. Got:\n{combined_output}"
