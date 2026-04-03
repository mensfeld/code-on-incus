"""
Test profile inheritance for environment maps.

Tests that environment maps merge correctly: child keys win, parent keys
preserved, and empty string clears inherited keys.
"""

import subprocess
from pathlib import Path


def test_profile_inheritance_env_merged(coi_binary, cleanup_containers, workspace_dir):
    """Child env key added alongside parent's keys."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[environment]\nEDITOR = "vim"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n\n[environment]\nNEW_VAR = "yes"\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "show", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "EDITOR" in output, f"Should inherit parent env var EDITOR. Got:\n{output}"
    assert "NEW_VAR" in output, f"Should have child env var NEW_VAR. Got:\n{output}"


def test_profile_inheritance_env_override(coi_binary, cleanup_containers, workspace_dir):
    """Child env key overrides same parent key."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[environment]\nRUST_BACKTRACE = "1"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n\n[environment]\nRUST_BACKTRACE = "full"\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "show", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "full" in output, f"Child should override parent RUST_BACKTRACE. Got:\n{output}"


def test_profile_inheritance_env_clear_with_empty(coi_binary, cleanup_containers, workspace_dir):
    """Child sets parent key to empty string to clear it."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[environment]\nSECRET = "abc"\nKEEP = "yes"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text('inherits = "parent"\n\n[environment]\nSECRET = ""\n')

    result = subprocess.run(
        [coi_binary, "profile", "show", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "SECRET" not in output, f"SECRET should be cleared. Got:\n{output}"
    assert "KEEP" in output, f"KEEP should be inherited. Got:\n{output}"


def test_profile_inheritance_env_parent_only(coi_binary, cleanup_containers, workspace_dir):
    """Child without [environment] gets all parent env vars."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[environment]\nPARENT_VAR = "hello"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text('inherits = "parent"\nimage = "coi"\n')

    result = subprocess.run(
        [coi_binary, "profile", "show", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "PARENT_VAR" in output, f"Should inherit all parent env. Got:\n{output}"
