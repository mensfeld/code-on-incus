"""
Test for coi shutdown - nonexistent container.

Tests that:
1. Run shutdown on a container that doesn't exist
2. Verify it handles the error gracefully
"""

import subprocess


def test_shutdown_nonexistent_container(coi_binary, cleanup_containers):
    """
    Test shutting down a container that doesn't exist.

    Flow:
    1. Run coi shutdown nonexistent-container-xyz
    2. Verify it fails or shows warning
    """
    result = subprocess.run(
        [coi_binary, "shutdown", "nonexistent-container-xyz-123"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail because container doesn't exist
    assert result.returncode != 0, (
        f"Shutdown of nonexistent container should fail. stdout: {result.stdout}"
    )

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "failed" in combined_output or "warning" in combined_output or "not" in combined_output
    ), f"Should show failure/warning message. Got:\n{result.stdout + result.stderr}"
