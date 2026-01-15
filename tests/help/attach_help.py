"""
Test for coi attach --help - help text validation.

Tests that:
1. Run coi attach --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_attach_help(coi_binary):
    """
    Test attach command help output.

    Flow:
    1. Run coi attach --help
    2. Verify exit code is 0
    3. Verify output contains usage and description
    """
    result = subprocess.run(
        [coi_binary, "attach", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Attach help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the attach command
    assert "attach" in output.lower(), f"Should mention attach command. Got:\n{output}"

    # Should mention sessions or containers
    assert "session" in output.lower() or "container" in output.lower(), (
        f"Should mention sessions or containers. Got:\n{output}"
    )

    # Should mention key flags
    assert "--slot" in output, f"Should document --slot flag. Got:\n{output}"
