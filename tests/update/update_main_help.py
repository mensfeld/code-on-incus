"""
Test that coi update --help lists both subcommands.
"""

import subprocess


def test_update_help(coi_binary):
    """
    Run coi update --help and verify core and patterns subcommands are listed.

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
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"
    assert "core" in output, f"Should list core subcommand. Got:\n{output}"
    assert "patterns" in output, f"Should list patterns subcommand. Got:\n{output}"
