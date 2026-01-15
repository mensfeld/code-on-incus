"""
Test for coi shell --help - help text validation.

Tests that:
1. Run coi shell --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_shell_help(coi_binary):
    """
    Test shell command help output.

    Flow:
    1. Run coi shell --help
    2. Verify exit code is 0
    3. Verify output contains usage, description, and flags
    """
    result = subprocess.run(
        [coi_binary, "shell", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Shell help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the shell command
    assert "shell" in output.lower(), f"Should mention shell command. Got:\n{output}"

    # Should mention key flags
    assert "--slot" in output, f"Should document --slot flag. Got:\n{output}"
    assert "--resume" in output, f"Should document --resume flag. Got:\n{output}"
    assert "--persistent" in output, f"Should document --persistent flag. Got:\n{output}"

    # Should contain Flags section
    assert "Flags:" in output, f"Should contain Flags section. Got:\n{output}"
