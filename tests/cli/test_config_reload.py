"""
Test that commands use the package-level config loaded by PersistentPreRunE
instead of reloading config independently (which loses --profile processing).

Bug #14: list, info, clean, persist, monitor, health all called config.Load()
locally, discarding the profile-merged config from PersistentPreRunE.
"""

import subprocess
from pathlib import Path


def test_list_respects_profile_flag(coi_binary, workspace_dir):
    """coi list --profile <name> should not error about unknown profile.

    Before the fix, list reloaded config independently, so the profile
    applied by PersistentPreRunE was thrown away and the local load didn't
    know about --profile.
    """
    # Create a minimal profile
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "testprofile"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('image = "coi-test"\n')

    result = subprocess.run(
        [
            coi_binary,
            "list",
            "--profile",
            "testprofile",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    combined = result.stdout + result.stderr

    # Should succeed (list may show 0 containers, that's fine)
    assert result.returncode == 0, (
        f"list --profile should succeed, got rc={result.returncode}: {combined}"
    )
    # Must not contain profile-related errors
    assert "unknown profile" not in combined.lower(), f"Profile not recognized: {combined}"
    assert "flag redefined" not in combined.lower(), f"Flag conflict: {combined}"


def test_info_respects_profile_flag(coi_binary, workspace_dir):
    """coi info --profile <name> should not error about unknown profile."""
    # Create a minimal profile
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "testprofile"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('image = "coi-test"\n')

    result = subprocess.run(
        [
            coi_binary,
            "info",
            "--profile",
            "testprofile",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    combined = result.stdout + result.stderr

    # info with no session ID may fail with "no sessions found" — that's OK.
    # What matters is it does NOT fail with a profile error.
    assert "unknown profile" not in combined.lower(), f"Profile not recognized: {combined}"
    assert "flag redefined" not in combined.lower(), f"Flag conflict: {combined}"
