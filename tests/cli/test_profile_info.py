"""
Test that 'profile show' has been renamed to 'profile info' with backward-compat alias.

Issue #2: Standardize on 'info' verb for showing single-item details.
"""

import subprocess


def test_profile_info_help(coi_binary):
    """coi profile info --help should succeed and mention 'info'."""
    result = subprocess.run(
        [coi_binary, "profile", "info", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "info" in combined.lower(), f"Expected 'info' in help output, got: {combined}"


def test_profile_show_still_works(coi_binary):
    """coi profile show --help should still work (backward compat alias)."""
    result = subprocess.run(
        [coi_binary, "profile", "show", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"


def test_profile_help_lists_info(coi_binary):
    """coi profile --help should list 'info' as a subcommand."""
    result = subprocess.run(
        [coi_binary, "profile", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "info" in combined, f"Expected 'info' subcommand listed in help, got: {combined}"
