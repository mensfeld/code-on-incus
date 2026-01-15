"""
Test for coi completion --help - completion help text validation.

Tests that:
1. Run coi completion --help
2. Verify help text explains how to use completion
3. Verify exit code is 0
"""

import subprocess


def test_completion_help(coi_binary):
    """
    Test completion command help output.

    Flow:
    1. Run coi completion --help
    2. Verify exit code is 0
    3. Verify output contains usage and supported shells
    """
    result = subprocess.run(
        [coi_binary, "completion", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Completion help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should mention completion
    assert "completion" in output.lower(), f"Should mention completion. Got:\n{output}"

    # Should list available shells
    assert "bash" in output.lower(), f"Should mention bash. Got:\n{output}"
    assert "zsh" in output.lower(), f"Should mention zsh. Got:\n{output}"
    assert "fish" in output.lower(), f"Should mention fish. Got:\n{output}"
