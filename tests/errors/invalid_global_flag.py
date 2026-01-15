"""
Test for coi --invalid-flag - error handling.

Tests that:
1. Run coi with an invalid global flag
2. Verify it returns non-zero exit code
3. Verify error message is helpful
"""

import subprocess


def test_invalid_global_flag(coi_binary):
    """
    Test behavior with invalid global flag.

    Flow:
    1. Run coi with non-existent flag
    2. Verify exit code is non-zero
    3. Verify error message mentions the invalid flag
    """
    result = subprocess.run(
        [coi_binary, "--nonexistent-flag"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode != 0, f"Invalid flag should fail. Got exit code: {result.returncode}"

    # Error message should appear in stderr
    error_output = result.stderr

    # Should mention the invalid flag
    assert "nonexistent-flag" in error_output or "unknown flag" in error_output.lower(), (
        f"Should mention invalid flag. Got:\n{error_output}"
    )
