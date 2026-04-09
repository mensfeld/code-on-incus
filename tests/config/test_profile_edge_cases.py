"""
Edge case tests for profile directory loading.

Tests that:
1. Empty profile directory (no config.toml) is silently skipped
2. Profile directory with invalid TOML causes a fatal error
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
    (valid_dir / "config.toml").write_text('[container]\nimage = "coi-default"\n')

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


def test_invalid_toml_profile_crashes(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a profile directory with invalid TOML causes a fatal error.
    """
    # Create profile with broken TOML
    bad_dir = Path(workspace_dir) / ".coi" / "profiles" / "broken"
    bad_dir.mkdir(parents=True)
    (bad_dir / "config.toml").write_text("[invalid toml {\n")

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

    assert result.returncode != 0, f"Should fail on invalid TOML profile. stdout: {result.stdout}"
    combined = result.stdout + result.stderr
    assert "broken" in combined.lower(), (
        f"Error should mention the broken profile. Got:\n{combined}"
    )


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
        (profile_dir / "config.toml").write_text(f'[container]\nimage = "img-{name}"\n')

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
[container]
image = "coi-default"

[container.build]
base = "coi-default"
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
            "info",
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
    Test that a profile with only a [container.build] section (no image, no env) works.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "buildonly"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[container.build]
base = "coi-default"
commands = ["apt-get install -y curl"]
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "info",
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


def test_profile_validation_missing_build_script(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that using a profile with a non-existent build script fails at runtime.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "badbuild"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[container]
image = "coi-default"

[container.build]
base = "coi-default"
script = "nonexistent.sh"
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "badbuild",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, (
        f"Should fail when build script doesn't exist. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "build script" in combined.lower(), (
        f"Error should mention build script. Got:\n{combined}"
    )


def test_profile_validation_invalid_network_mode(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that using a profile with an invalid network mode fails at runtime.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "badnet"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[container]
image = "coi-default"

[network]
mode = "bogus"
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "badnet",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, f"Should fail with invalid network mode. stdout: {result.stdout}"
    combined = result.stdout + result.stderr
    assert "network mode" in combined.lower(), (
        f"Error should mention invalid network mode. Got:\n{combined}"
    )


def test_profile_validation_missing_context_file(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that using a profile with a non-existent context file fails at runtime.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "badcontext"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
context = "CONTEXT.md"

[container]
image = "coi-default"
"""
    )
    # Note: CONTEXT.md is NOT created, so it should fail validation

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "badcontext",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, (
        f"Should fail when context file doesn't exist. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "context file" in combined.lower(), (
        f"Error should mention context file. Got:\n{combined}"
    )


def test_profile_context_file_loads_when_present(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a profile with a valid context file loads and is shown in profile show.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "withcontext"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
context = "CONTEXT.md"

[container]
image = "coi-default"
"""
    )
    (profile_dir / "CONTEXT.md").write_text("# My Profile Context\nUse pytest.\n")

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "info",
            "withcontext",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile show should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "context" in output.lower(), f"Should show context field. Got:\n{output}"
    assert "CONTEXT.md" in output, f"Should show context file path. Got:\n{output}"


def test_profile_validation_incomplete_mount(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that using a profile with an incomplete mount entry fails at runtime.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "badmount"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[container]
image = "coi-default"

[[mounts]]
host = "~/data"
# missing container path
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "badmount",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, f"Should fail with incomplete mount. stdout: {result.stdout}"
    combined = result.stdout + result.stderr
    assert "container" in combined.lower(), (
        f"Error should mention missing container path. Got:\n{combined}"
    )
