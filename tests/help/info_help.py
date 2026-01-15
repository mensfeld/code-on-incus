"""
Test for coi info --help - help text validation.

Tests that:
1. Run coi info --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_info_help(coi_binary):
    """
    Test info command help output.

    Flow:
    1. Run coi info --help
    2. Verify exit code is 0
    3. Verify output contains usage and description
    """
    result = subprocess.run(
        [coi_binary, "info", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Info help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the info command
    assert "info" in output.lower(), f"Should mention info command. Got:\n{output}"

    # Should mention session information
    assert "session" in output.lower(), f"Should mention sessions. Got:\n{output}"
