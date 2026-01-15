"""
Test for coi <invalid-command> - error handling.

Tests that:
1. Run coi with an invalid command
2. Verify it returns non-zero exit code
3. Verify error message is helpful
"""

import subprocess


def test_invalid_command(coi_binary):
    """
    Test behavior with invalid command.

    Flow:
    1. Run coi with non-existent command
    2. Verify exit code is non-zero
    3. Verify error message mentions the invalid command
    """
    result = subprocess.run(
        [coi_binary, "nonexistent-command"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode != 0, (
        f"Invalid command should fail. Got exit code: {result.returncode}"
    )

    # Error message should appear in stderr
    error_output = result.stderr

    # Should mention the invalid command
    assert "nonexistent-command" in error_output or "unknown command" in error_output.lower(), (
        f"Should mention invalid command. Got:\n{error_output}"
    )

    # Should suggest using help
    assert "help" in error_output.lower() or "--help" in error_output, (
        f"Should suggest using help. Got:\n{error_output}"
    )
