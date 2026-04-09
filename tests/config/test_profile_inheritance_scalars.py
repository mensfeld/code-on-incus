"""
Test profile inheritance for scalar fields.

Tests that image and persistent are inherited/overridden correctly.
"""

import subprocess
from pathlib import Path


def test_profile_inheritance_image_from_parent(coi_binary, cleanup_containers, workspace_dir):
    """Child without image gets parent's image."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[container]\nimage = "coi-parent"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text('inherits = "parent"\n')

    result = subprocess.run(
        [coi_binary, "profile", "info", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-parent" in output, f"Should inherit parent image. Got:\n{output}"


def test_profile_inheritance_image_overridden(coi_binary, cleanup_containers, workspace_dir):
    """Child with image overrides parent's image."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[container]\nimage = "coi-parent"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n[container]\nimage = "coi-child"\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "info", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-child" in output, f"Should show child's overridden image. Got:\n{output}"


def test_profile_inheritance_persistent_from_parent(coi_binary, cleanup_containers, workspace_dir):
    """Child inherits parent's persistent flag."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[container]\nimage = "coi-default"\npersistent = true\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text('inherits = "parent"\n')

    result = subprocess.run(
        [coi_binary, "profile", "info", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "true" in output, f"Should inherit persistent=true. Got:\n{output}"
