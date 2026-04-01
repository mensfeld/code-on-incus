"""
Edge case tests for profile directory loading.

Tests that:
1. Empty profile directory (no config.toml) is silently skipped
2. Profile directory with invalid TOML is skipped without crashing
3. Profile directory with only non-config files is ignored
4. Multiple profiles from directories work simultaneously
5. Non-existent build script in profile does not prevent profile from loading
6. Profile with only build section (no image) works
"""

import subprocess
from pathlib import Path


def test_empty_profile_directory_skipped(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that an empty profile directory (no config.toml) is silently ignored.
    """
    # Create profile directory with no config.toml
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "emptydir"
    profile_dir.mkdir(parents=True)

    # Also create a valid profile to confirm loading still works
    valid_dir = Path(workspace_dir) / ".coi" / "profiles" / "valid"
    valid_dir.mkdir(parents=True)
    (valid_dir / "config.toml").write_text('image = "coi"\n')

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "list",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "valid" in output, f"Valid profile should be listed. Got:\n{output}"
    assert "emptydir" not in output, f"Empty directory should not appear as profile. Got:\n{output}"


def test_invalid_toml_profile_skipped(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a profile directory with invalid TOML is skipped without error.
    """
    # Create profile with broken TOML
    bad_dir = Path(workspace_dir) / ".coi" / "profiles" / "broken"
    bad_dir.mkdir(parents=True)
    (bad_dir / "config.toml").write_text("[invalid toml {\n")

    # Create a valid profile
    valid_dir = Path(workspace_dir) / ".coi" / "profiles" / "good"
    valid_dir.mkdir(parents=True)
    (valid_dir / "config.toml").write_text('image = "coi"\n')

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "list",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Should succeed despite broken profile. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "good" in output, f"Valid profile should still load. Got:\n{output}"
    assert "broken" not in output, f"Broken profile should not appear. Got:\n{output}"


def test_profile_dir_with_non_config_files_only(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a profile directory with files but no config.toml is ignored.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "justscripts"
    profile_dir.mkdir(parents=True)
    (profile_dir / "build.sh").write_text("#!/bin/bash\necho hi\n")
    (profile_dir / "setup.sh").write_text("#!/bin/bash\necho setup\n")
    # No config.toml

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "list",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "justscripts" not in output, (
        f"Profile without config.toml should not appear. Got:\n{output}"
    )


def test_multiple_directory_profiles(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that multiple directory profiles load and are all listed.
    """
    for name in ["alpha", "beta", "gamma"]:
        profile_dir = Path(workspace_dir) / ".coi" / "profiles" / name
        profile_dir.mkdir(parents=True)
        (profile_dir / "config.toml").write_text(f'image = "img-{name}"\n')

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "list",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    for name in ["alpha", "beta", "gamma"]:
        assert name in output, f"Profile '{name}' should be listed. Got:\n{output}"
        assert f"img-{name}" in output, f"Image for '{name}' should be shown. Got:\n{output}"


def test_profile_nonexistent_build_script_still_loads(
    coi_binary, cleanup_containers, workspace_dir
):
    """
    Test that a profile referencing a non-existent build script still loads.
    The script existence check happens at build time, not at config load time.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "missingscript"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
image = "coi"

[build]
base = "coi"
script = "this-script-does-not-exist.sh"
"""
    )

    # Profile should load and be visible
    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "list",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "missingscript" in output, (
        f"Profile with missing script should still be listed. Got:\n{output}"
    )

    # Profile show should work too
    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "show",
            "missingscript",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile show should succeed. stderr: {result.stderr}"


def test_profile_with_only_build_section(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a profile with only a [build] section (no image, no env) works.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "buildonly"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[build]
base = "coi"
commands = ["apt-get install -y curl"]
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "show",
            "buildonly",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "buildonly" in output, f"Should show profile name. Got:\n{output}"
    assert "apt-get" in output, f"Should show build commands. Got:\n{output}"


def test_profile_file_in_profiles_dir_not_treated_as_profile(
    coi_binary, cleanup_containers, workspace_dir
):
    """
    Test that a regular file (not directory) inside profiles/ is ignored.
    """
    profiles_dir = Path(workspace_dir) / ".coi" / "profiles"
    profiles_dir.mkdir(parents=True)
    # Create a file, not a directory
    (profiles_dir / "not-a-profile.toml").write_text('image = "test"\n')

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "list",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    # Should show "No profiles" since there are no valid profile directories
    assert "not-a-profile" not in output, (
        f"Regular file should not be treated as profile. Got:\n{output}"
    )
