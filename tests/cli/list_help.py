"""
Test list subcommand help.

Expected:
- list --help works
"""

import subprocess


def test_list_help(coi_binary):
    """Test that coi list --help works."""
    result = subprocess.run(
        [coi_binary, "list", "--help"], capture_output=True, text=True, timeout=5
    )

    assert result.returncode == 0
    assert "list" in result.stdout.lower()
