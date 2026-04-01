"""
Test for coi update --help - help text validation.

Tests that:
1. Run coi update --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_update_help(coi_binary):
    """
    Test update command help output.

    Flow:
    1. Run coi update --help
    2. Verify exit code is 0
    3. Verify output contains usage, description, and key flags
    """
    result = subprocess.run(
        [coi_binary, "update", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Update help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the update command
    assert "update" in output.lower(), f"Should mention update command. Got:\n{output}"

    # Should mention GitHub
    assert "GitHub" in output or "github" in output.lower(), (
        f"Should mention GitHub. Got:\n{output}"
    )

    # Should document --check flag
    assert "--check" in output, f"Should document --check flag. Got:\n{output}"

    # Should document --force flag
    assert "--force" in output, f"Should document --force flag. Got:\n{output}"

    # Should describe what --check does
    assert "check" in output.lower(), f"Should describe check functionality. Got:\n{output}"

    # --version flag should be hidden (not shown in help)
    assert "--version" not in output, (
        f"Hidden --version flag should not appear in help. Got:\n{output}"
    )
