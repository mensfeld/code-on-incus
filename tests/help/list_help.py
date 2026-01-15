"""
Test for coi list --help - help text validation.

Tests that:
1. Run coi list --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_list_help(coi_binary):
    """
    Test list command help output.

    Flow:
    1. Run coi list --help
    2. Verify exit code is 0
    3. Verify output contains usage and flags
    """
    result = subprocess.run(
        [coi_binary, "list", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"List help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the list command
    assert "list" in output.lower(), f"Should mention list command. Got:\n{output}"

    # Should mention key flags
    assert "--all" in output, f"Should document --all flag. Got:\n{output}"
    assert "--format" in output, f"Should document --format flag. Got:\n{output}"

    # Should mention what it lists
    assert "container" in output.lower(), f"Should mention containers. Got:\n{output}"
    assert "session" in output.lower(), f"Should mention sessions. Got:\n{output}"
