"""
Test profile-specific build configuration.

Tests that:
1. Profile [build] section with script is resolved relative to profile directory
2. Non-existent build script in profile produces a clear error
3. Profile build commands work as alternative to script
"""

import subprocess
from pathlib import Path


def test_profile_build_script_relative_resolution(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that build.script in a profile directory is resolved relative to that directory.

    We don't actually run a build, but verify that coi profile show displays
    the fully resolved script path.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "buildtest"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[build]
base = "coi-default"
script = "build.sh"
"""
    )
    # Create the actual build script so it's valid
    build_script = profile_dir / "build.sh"
    build_script.write_text("#!/bin/bash\necho 'building...'\n")
    build_script.chmod(0o755)

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "show",
            "buildtest",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile show should succeed. stderr: {result.stderr}"
    combined = result.stdout + result.stderr
    # The script path should be fully resolved to the profile directory
    expected_path = str(profile_dir / "build.sh")
    assert expected_path in combined, (
        f"Build script should be resolved to {expected_path}. Got:\n{combined}"
    )


def test_profile_nonexistent_build_script(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that referencing a non-existent build script in a profile is handled.

    The profile should still load (build script existence is checked at build time,
    not at config load time), but coi build should fail gracefully.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "badscript"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
image = "coi-nonexistent-image-xyz"

[build]
base = "coi-default"
script = "does-not-exist.sh"
"""
    )

    # Profile should still load — profile show should work
    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "show",
            "badscript",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, (
        f"profile show should succeed even with missing build script. stderr: {result.stderr}"
    )
    combined = result.stdout + result.stderr
    assert "does-not-exist.sh" in combined, (
        f"Should show the configured script path. Got:\n{combined}"
    )

    # But trying to build should fail since the script doesn't exist
    result = subprocess.run(
        [
            coi_binary,
            "build",
            "--profile",
            "badscript",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, (
        f"Build with non-existent script should fail. stdout: {result.stdout}"
    )


def test_profile_build_commands(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that profile with build commands (inline) loads correctly.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "cmdprofile"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[build]
base = "coi-default"
commands = ["echo hello", "echo world"]
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "show",
            "cmdprofile",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile show should succeed. stderr: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "echo hello" in combined, f"Build commands should be displayed. Got:\n{combined}"
