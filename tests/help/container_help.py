"""
Test for coi container --help - help text validation.

Tests that:
1. Run coi container --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_container_help(coi_binary):
    """
    Test container command help output.

    Flow:
    1. Run coi container --help
    2. Verify exit code is 0
    3. Verify output contains subcommands
    """
    result = subprocess.run(
        [coi_binary, "container", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Container help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the container command
    assert "container" in output.lower(), f"Should mention container command. Got:\n{output}"

    # Should list subcommands
    assert "Available Commands:" in output or "Commands:" in output, (
        f"Should list subcommands. Got:\n{output}"
    )

    # Should mention common container operations
    assert "launch" in output.lower() or "start" in output.lower() or "exec" in output.lower(), (
        f"Should mention container operations. Got:\n{output}"
    )
