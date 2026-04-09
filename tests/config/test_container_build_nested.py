"""
Test that [container.build] section parses correctly.

Tests that base, script, and commands fields under [container.build] load
and that script paths are resolved relative to the config file.
"""

import subprocess
from pathlib import Path


def test_container_build_nested_parses(coi_binary, workspace_dir):
    """[container.build] base/script/commands fields parse in a profile."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "buildtest"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        "[container]\n"
        'image = "coi-build-nested"\n\n'
        "[container.build]\n"
        'base = "coi-default"\n'
        'script = "build.sh"\n'
    )
    (profile_dir / "build.sh").write_text("#!/bin/bash\necho hello\n")

    result = subprocess.run(
        [coi_binary, "profile", "info", "buildtest", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-build-nested" in output, f"Should show image. Got:\n{output}"
    assert "coi-default" in output, f"Should show build base. Got:\n{output}"
    assert "build.sh" in output, f"Should show build script. Got:\n{output}"


def test_container_build_script_resolved_relative(coi_binary, workspace_dir):
    """Build script path is resolved relative to the profile's config file."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "relbuild"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        '[container]\nimage = "coi-relbuild"\n\n[container.build]\nscript = "build.sh"\n'
    )
    (profile_dir / "build.sh").write_text("#!/bin/bash\necho hi\n")

    result = subprocess.run(
        [coi_binary, "profile", "info", "relbuild", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    # The displayed script path should be the absolute path resolved against
    # the profile directory.
    expected_script = str(profile_dir / "build.sh")
    assert expected_script in output, (
        f"Build script path should resolve to absolute path under profile dir.\n"
        f"Expected substring: {expected_script}\nGot:\n{output}"
    )


def test_container_build_commands_inline(coi_binary, workspace_dir):
    """[container.build] commands array parses for inline command builds."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "cmdbuild"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        "[container]\n"
        'image = "coi-cmdbuild"\n\n'
        "[container.build]\n"
        'commands = ["echo step1", "echo step2"]\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "info", "cmdbuild", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "echo step1" in output, f"Should show first command. Got:\n{output}"
    assert "echo step2" in output, f"Should show second command. Got:\n{output}"
