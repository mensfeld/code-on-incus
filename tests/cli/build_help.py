"""
Test build subcommand help.

Expected:
- build --help works
"""

import subprocess


def test_build_help(coi_binary):
    """Test that coi build --help works."""
    result = subprocess.run(
        [coi_binary, "build", "--help"], capture_output=True, text=True, timeout=5
    )

    assert result.returncode == 0
    assert "build" in result.stdout.lower()
