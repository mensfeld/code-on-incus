"""
Test for coi attach - nonexistent container.

Tests that:
1. Run coi attach with a container name that doesn't exist
2. Verify it shows an error message
"""

import subprocess


def test_attach_nonexistent_container(coi_binary, cleanup_containers):
    """
    Test that coi attach with invalid container name shows error.

    Flow:
    1. Run coi attach with a fake container name
    2. Verify it returns error about container not found
    """
    fake_container = "coi-nonexistent-99999"

    result = subprocess.run(
        [coi_binary, "attach", fake_container],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail
    assert result.returncode != 0, (
        f"coi attach should fail for nonexistent container. stdout: {result.stdout}"
    )

    combined_output = (result.stdout + result.stderr).lower()
    assert "not found" in combined_output or "not running" in combined_output, (
        f"Should show 'not found' or 'not running' error. Got:\nstdout: {result.stdout}\nstderr: {result.stderr}"
    )
