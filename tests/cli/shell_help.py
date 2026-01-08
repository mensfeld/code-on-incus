"""
Test shell subcommand help.

Expected:
- shell --help works and shows usage
"""

import subprocess


def test_shell_help(coi_binary):
    """Test that coi shell --help works."""
    result = subprocess.run(
        [coi_binary, "shell", "--help"], capture_output=True, text=True, timeout=5
    )

    assert result.returncode == 0
    assert "shell" in result.stdout.lower()
    assert "usage:" in result.stdout.lower()
