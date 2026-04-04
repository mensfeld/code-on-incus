"""
Test that 'coi images' is deprecated but still functional.

Issue #3: coi images (plural) coexists with coi image list — should be hidden as deprecated alias.
"""

import subprocess


def test_images_still_works(coi_binary):
    """coi images should still exit 0 (backward compatibility)."""
    result = subprocess.run(
        [coi_binary, "images"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Expected exit 0 for deprecated 'coi images', got {result.returncode}: {result.stderr}"
    )


def test_images_hidden_from_help(coi_binary):
    """coi --help should NOT list 'images' as a command (hidden)."""
    result = subprocess.run(
        [coi_binary, "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    # The word "images" (plural, as a standalone command) should not appear in help
    # Check that "images" doesn't appear as a listed command
    # "image" (singular) should still appear
    lines = combined.split("\n")
    command_lines = [line.strip() for line in lines if line.strip().startswith("images ")]
    assert len(command_lines) == 0, (
        f"'images' command should be hidden from help, but found: {command_lines}"
    )
