"""
Test for coi container running - returns false for nonexistent container.

Tests that:
1. Check running for nonexistent container
2. Verify it returns non-zero (false)
"""

import subprocess


def test_running_nonexistent(coi_binary, cleanup_containers):
    """
    Test that running returns non-zero for nonexistent container.

    Flow:
    1. Check running for a container that doesn't exist
    2. Verify exit code is non-zero (false)
    """
    nonexistent_name = "nonexistent-container-12345"

    # === Phase 1: Check running ===

    result = subprocess.run(
        [coi_binary, "container", "running", nonexistent_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify returns false ===

    assert result.returncode != 0, "Running should return non-zero for nonexistent container"
