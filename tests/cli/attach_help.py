"""
Test attach subcommand help.

Expected:
- attach --help works
"""

import subprocess


def test_attach_help(coi_binary):
    """Test that coi attach --help works."""
    result = subprocess.run(
        [coi_binary, "attach", "--help"], capture_output=True, text=True, timeout=5
    )

    assert result.returncode == 0
    assert "attach" in result.stdout.lower()
