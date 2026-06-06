"""
Test for coi update --help - help text validation.

Tests that:
1. Run coi update --help and verify subcommands are listed
2. Run coi update core --help and verify core update flags
3. Verify exit codes are 0
"""

import subprocess


def test_update_help(coi_binary):
    """
    Test update command help output shows subcommands.

    Flow:
    1. Run coi update --help
    2. Verify exit code is 0
    3. Verify output lists core and patterns subcommands
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

    # Should list the core subcommand
    assert "core" in output, f"Should list core subcommand. Got:\n{output}"

    # Should list the patterns subcommand
    assert "patterns" in output, f"Should list patterns subcommand. Got:\n{output}"


def test_update_core_help(coi_binary):
    """
    Test update core subcommand help output.

    Flow:
    1. Run coi update core --help
    2. Verify exit code is 0
    3. Verify output contains GitHub reference, --check, and --force flags
    """
    result = subprocess.run(
        [coi_binary, "update", "core", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Update core help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should mention GitHub
    assert "GitHub" in output or "github" in output.lower(), (
        f"Should mention GitHub. Got:\n{output}"
    )

    # Should document --check flag
    assert "--check" in output, f"Should document --check flag. Got:\n{output}"

    # Should document --force flag
    assert "--force" in output, f"Should document --force flag. Got:\n{output}"

    # --version flag should be hidden (not shown in help)
    assert "--version" not in output, (
        f"Hidden --version flag should not appear in help. Got:\n{output}"
    )
