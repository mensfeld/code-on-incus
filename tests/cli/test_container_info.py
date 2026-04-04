"""
Test that 'coi container info' subcommand exists and is accessible.

Issue #2: container lacked an info subcommand while snapshot info and coi info exist.
"""

import subprocess


def test_container_info_help(coi_binary):
    """coi container info --help should succeed and contain 'info'."""
    result = subprocess.run(
        [coi_binary, "container", "info", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "info" in combined.lower(), f"Expected 'info' in help output, got: {combined}"


def test_container_help_lists_info(coi_binary):
    """coi container --help should list the info subcommand."""
    result = subprocess.run(
        [coi_binary, "container", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "info" in combined, f"Expected 'info' listed in container help, got: {combined}"
