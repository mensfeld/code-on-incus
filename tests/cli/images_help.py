"""
Test images subcommand help.

Expected:
- images --help works
"""

import subprocess


def test_images_help(coi_binary):
    """Test that coi images --help works."""
    result = subprocess.run(
        [coi_binary, "images", "--help"], capture_output=True, text=True, timeout=5
    )

    assert result.returncode == 0
    assert "images" in result.stdout.lower()
