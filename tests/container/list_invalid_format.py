"""
Test for coi container list - invalid format error handling.

Tests that:
1. Container list rejects invalid format values
2. Error message is clear
"""

import subprocess


def test_container_list_invalid_format(coi_binary):
    """
    Test container list with invalid format.

    Flow:
    1. Run coi container list --format=invalid
    2. Verify command fails with exit code 2 (usage error)
    3. Verify error message mentions invalid format
    """
    # === Test: Try invalid format ===

    result = subprocess.run(
        [coi_binary, "container", "list", "--format=invalid"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail with exit code 2 (usage error)
    assert result.returncode == 2, (
        f"Invalid format should fail with exit code 2. Got exit code: {result.returncode}"
    )

    # Error message should mention the invalid format
    combined_output = result.stdout + result.stderr
    assert "invalid format" in combined_output.lower(), (
        f"Error message should mention invalid format. Got output:\n{combined_output}"
    )
