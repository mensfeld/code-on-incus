"""
Test that 'coi image info' subcommand exists and is accessible.

Issue #2: image lacked an info subcommand while snapshot info and coi info exist.
"""

import subprocess


def test_image_info_help(coi_binary):
    """coi image info --help should succeed and contain 'info'."""
    result = subprocess.run(
        [coi_binary, "image", "info", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "info" in combined.lower(), f"Expected 'info' in help output, got: {combined}"


def test_image_help_lists_info(coi_binary):
    """coi image --help should list the info subcommand."""
    result = subprocess.run(
        [coi_binary, "image", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "info" in combined, f"Expected 'info' listed in image help, got: {combined}"


def test_image_info_format_invalid_rejected(coi_binary):
    """coi image info --format xml should fail with exit code 2."""
    result = subprocess.run(
        [coi_binary, "image", "info", "--format", "xml", "nonexistent"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, f"Expected exit code 2 for --format xml, got {result.returncode}"
    combined = result.stdout + result.stderr
    assert "invalid format" in combined.lower(), f"Expected 'invalid format' error, got: {combined}"
