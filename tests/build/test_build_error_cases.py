"""
Test error and edge cases for build-from-config.

Tests:
- [build] with defaults.image = "coi-default" → falls back to base build (no config build)
- [build] with no defaults.image set → falls back to base build
- [build] with base image that doesn't exist → clear error
- [build] with empty commands array → falls back to base build
- [build] with only base= set (no script/commands) → falls back to base build
- Missing script via explicit 'coi build' → clear error
- Commands that fail during build → build error propagated
"""

import subprocess
from pathlib import Path

import pytest


def skip_without_coi(coi_binary):
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        pytest.skip("coi base image not found")


def test_build_config_image_is_coi_falls_back(coi_binary, workspace_dir):
    """
    [build] with defaults.image = "coi-default" should NOT trigger config build.
    Falls back to building the base coi image.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
image = "coi-default"

[build]
commands = ["echo should-not-be-used-as-custom-build"]
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )

    # Should succeed — falls back to building base coi image
    assert result.returncode == 0, f"Should fall back to base build. stderr: {result.stderr}"
    combined = result.stdout + result.stderr
    # Should NOT be treating this as a custom image build
    assert "custom image 'coi-default'" not in combined.lower(), (
        f"Should build base coi-default image, not custom. Got:\n{combined}"
    )


def test_build_config_no_image_set_falls_back(coi_binary, workspace_dir):
    """
    [build] without defaults.image set → falls back to base coi build.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[build]
commands = ["echo orphan-build-config"]
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )

    # Should succeed — falls back to building base coi image
    assert result.returncode == 0, f"Should fall back to base build. stderr: {result.stderr}"


def test_build_config_nonexistent_base_image(coi_binary, workspace_dir):
    """
    [build] with base image that doesn't exist → clear error.
    """
    image_name = "coi-test-bad-base-explicit"

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
base = "coi-nonexistent-base-99999"
commands = ["echo hello"]
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, (
        f"Build with nonexistent base should fail. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert (
        "fail" in combined.lower() or "error" in combined.lower() or "not found" in combined.lower()
    ), f"Should indicate build failure due to missing base. Got:\n{combined}"


def test_build_config_empty_commands_errors(coi_binary, workspace_dir):
    """
    [build] with commands = [] (empty array) → no valid build config,
    errors because custom image has no build script or commands.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
image = "coi-test-empty-cmds"

[build]
commands = []
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )

    # Empty commands with custom image name → error (no build config for custom image)
    assert result.returncode != 0, (
        f"Empty commands with custom image should error. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "build" in combined.lower(), f"Error should mention build. Got:\n{combined}"


def test_build_config_only_base_no_script_or_commands(coi_binary, workspace_dir):
    """
    [build] with only base= set (no script or commands) → errors.
    base= alone is not enough to constitute a build config for a custom image.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
image = "coi-test-base-only"

[build]
base = "coi-default"
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )

    # base= alone with custom image → error (no script or commands to execute)
    assert result.returncode != 0, (
        f"base= alone with custom image should error. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "build" in combined.lower(), f"Error should mention build. Got:\n{combined}"


def test_build_missing_script_explicit_build(coi_binary, workspace_dir):
    """
    'coi build' with script pointing to nonexistent file → clear error.
    """
    image_name = "coi-test-build-missing-script"

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
script = "nonexistent-build.sh"
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Build should fail with missing script"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention script not found. Got:\n{combined}"
    )


def test_build_script_that_fails(coi_binary, workspace_dir):
    """
    Build script that exits non-zero → build fails with clear error.
    """
    skip_without_coi(coi_binary)
    image_name = "coi-test-failing-script"

    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
script = "failing-build.sh"
"""
    )

    # Create a script that fails
    build_script = config_dir / "failing-build.sh"
    build_script.write_text(
        """#!/bin/bash
set -e
echo "About to fail..."
exit 1
"""
    )
    build_script.chmod(0o755)

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Build should fail when script exits non-zero"
    combined = result.stdout + result.stderr
    assert "fail" in combined.lower(), f"Error should indicate build failure. Got:\n{combined}"


def test_build_commands_that_fail(coi_binary, workspace_dir):
    """
    Inline commands that fail → build fails with clear error.
    """
    skip_without_coi(coi_binary)
    image_name = "coi-test-failing-commands"

    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
commands = ["echo starting", "false", "echo should-not-reach"]
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Build should fail when commands exit non-zero"
    combined = result.stdout + result.stderr
    assert "fail" in combined.lower(), f"Error should indicate build failure. Got:\n{combined}"


def test_build_no_config_no_build_section(coi_binary, workspace_dir):
    """
    'coi build' with .coi/config.toml but no [build] section → builds base coi image.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
image = "coi-default"
persistent = true
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, (
        f"Should build base coi image when no [build] section. stderr: {result.stderr}"
    )
    combined = result.stdout + result.stderr
    assert "coi" in combined.lower(), f"Should mention building coi. Got:\n{combined}"
