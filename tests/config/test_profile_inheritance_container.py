"""
Test profile inheritance for the [container] section.

Verifies that the new [container] block deep-merges field-by-field:
- image inherited when child doesn't set it
- storage_pool overridden by child
- persistent *bool semantics preserved through merge
- [container.build] merges field-by-field
"""

import subprocess
from pathlib import Path


def test_inheritance_container_image_from_parent(coi_binary, workspace_dir):
    """Child without [container] image inherits parent's image."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent = coi_dir / "parent"
    parent.mkdir(parents=True)
    (parent / "config.toml").write_text(
        '[container]\nimage = "coi-parent-img"\nstorage_pool = "parent-pool"\n'
    )

    child = coi_dir / "child"
    child.mkdir(parents=True)
    (child / "config.toml").write_text('inherits = "parent"\n')

    result = subprocess.run(
        [coi_binary, "profile", "info", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-parent-img" in output, f"Should inherit image. Got:\n{output}"
    assert "parent-pool" in output, f"Should inherit storage_pool. Got:\n{output}"


def test_inheritance_container_storage_pool_overridden(coi_binary, workspace_dir):
    """Child overrides storage_pool, parent's image kept."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent = coi_dir / "parent"
    parent.mkdir(parents=True)
    (parent / "config.toml").write_text(
        '[container]\nimage = "coi-parent-img"\nstorage_pool = "parent-pool"\n'
    )

    child = coi_dir / "child"
    child.mkdir(parents=True)
    (child / "config.toml").write_text(
        'inherits = "parent"\n\n[container]\nstorage_pool = "child-pool"\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "info", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-parent-img" in output, f"Should keep parent image. Got:\n{output}"
    assert "child-pool" in output, f"Should show child storage_pool. Got:\n{output}"
    assert "parent-pool" not in output, f"Parent storage_pool should be overridden. Got:\n{output}"


def test_inheritance_container_persistent_from_parent(coi_binary, workspace_dir):
    """Child without persistent inherits parent's persistent flag."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent = coi_dir / "parent"
    parent.mkdir(parents=True)
    (parent / "config.toml").write_text('[container]\nimage = "coi-default"\npersistent = true\n')

    child = coi_dir / "child"
    child.mkdir(parents=True)
    (child / "config.toml").write_text('inherits = "parent"\n')

    result = subprocess.run(
        [coi_binary, "profile", "info", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "true" in output.lower(), f"Should inherit persistent=true. Got:\n{output}"


def test_inheritance_container_build_field_by_field(coi_binary, workspace_dir):
    """[container.build] merges field-by-field across inheritance."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent = coi_dir / "parent"
    parent.mkdir(parents=True)
    (parent / "config.toml").write_text(
        "[container]\n"
        'image = "coi-parent"\n\n'
        "[container.build]\n"
        'base = "coi-default"\n'
        'script = "parent.sh"\n'
    )
    (parent / "parent.sh").write_text("#!/bin/bash\necho parent\n")

    child = coi_dir / "child"
    child.mkdir(parents=True)
    (child / "config.toml").write_text(
        'inherits = "parent"\n\n[container.build]\nscript = "child.sh"\n'
    )
    (child / "child.sh").write_text("#!/bin/bash\necho child\n")

    result = subprocess.run(
        [coi_binary, "profile", "info", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-default" in output, f"Should inherit build base. Got:\n{output}"
    assert "child.sh" in output, f"Should override build script. Got:\n{output}"
    assert "parent.sh" not in output, f"Child script should replace parent's. Got:\n{output}"
