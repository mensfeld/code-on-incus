"""
Test for coi kill - kill nonexistent container.

Tests that:
1. Try to kill a container that doesn't exist
2. Verify appropriate error/warning
"""

import subprocess


def test_kill_nonexistent_container(coi_binary, cleanup_containers):
    """
    Test killing a nonexistent container.

    Flow:
    1. Try to kill nonexistent container
    2. Verify it fails or shows warning
    """
    fake_container = "coi-nonexistent-kill-test-99999"

    result = subprocess.run(
        [coi_binary, "kill", fake_container],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should either fail or show warning about no containers killed
    combined_output = result.stdout + result.stderr

    if result.returncode != 0:
        # Expected - failed to kill
        assert (
            "failed" in combined_output.lower()
            or "warning" in combined_output.lower()
            or "error" in combined_output.lower()
        ), f"Should show error message. Got:\n{combined_output}"
    else:
        # Also acceptable - showed warning but exited 0
        assert (
            "No containers were killed" in combined_output or "warning" in combined_output.lower()
        ), f"Should indicate no containers killed. Got:\n{combined_output}"
