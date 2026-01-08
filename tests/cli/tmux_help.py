"""
Test tmux subcommand help.

Expected:
- tmux --help works
"""

import subprocess


def test_tmux_help(coi_binary):
    """Test that coi tmux --help works."""
    result = subprocess.run(
        [coi_binary, "tmux", "--help"], capture_output=True, text=True, timeout=5
    )

    assert result.returncode == 0
    assert "tmux" in result.stdout.lower()
