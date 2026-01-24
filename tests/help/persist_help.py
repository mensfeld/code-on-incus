"""
Test for coi persist --help - help text validation.

Tests that:
1. Run coi persist --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_persist_help(coi_binary):
    """
    Test persist command help output.

    Flow:
    1. Run coi persist --help
    2. Verify exit code is 0
    3. Verify output contains usage and flags
    """
    result = subprocess.run(
        [coi_binary, "persist", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Persist help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the persist command
    assert "persist" in output.lower(), f"Should mention persist command. Got:\n{output}"

    # Should mention key flags
    assert "--all" in output, f"Should document --all flag. Got:\n{output}"
    assert "--force" in output, f"Should document --force flag. Got:\n{output}"

    # Should describe what it does
    assert "persistent" in output.lower(), f"Should mention persistent mode. Got:\n{output}"
    assert "ephemeral" in output.lower(), f"Should mention ephemeral mode. Got:\n{output}"

    # Should have examples
    assert "Examples:" in output, f"Should contain examples section. Got:\n{output}"
