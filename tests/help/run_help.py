"""
Test for coi run --help - help text validation.

Tests that:
1. Run coi run --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_run_help(coi_binary):
    """
    Test run command help output.

    Flow:
    1. Run coi run --help
    2. Verify exit code is 0
    3. Verify output contains usage, description, and examples
    """
    result = subprocess.run(
        [coi_binary, "run", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Run help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the run command
    assert "run" in output.lower(), f"Should mention run command. Got:\n{output}"

    # Should mention ephemeral nature
    assert "ephemeral" in output.lower(), f"Should mention ephemeral containers. Got:\n{output}"

    # Should contain example
    assert "Example" in output or "example" in output, f"Should contain example. Got:\n{output}"
