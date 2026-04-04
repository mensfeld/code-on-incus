"""
Test that 'coi images' (plural) has been removed.

Issue #3: coi images (plural) coexisted with coi image list — now removed.
"""

import subprocess


def test_images_not_recognized(coi_binary):
    """coi images should fail (command removed)."""
    result = subprocess.run(
        [coi_binary, "images"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, (
        f"Expected non-zero exit for removed 'coi images', got {result.returncode}"
    )


def test_images_not_in_help(coi_binary):
    """coi --help should NOT list 'images' as a command."""
    result = subprocess.run(
        [coi_binary, "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    lines = combined.split("\n")
    command_lines = [line.strip() for line in lines if line.strip().startswith("images ")]
    assert len(command_lines) == 0, (
        f"'images' command should not appear in help, but found: {command_lines}"
    )
