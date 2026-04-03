"""
Test profile inheritance for struct sections.

Tests that struct pointers (limits, tool, network) deep merge field by field.
"""

import subprocess
from pathlib import Path


def test_profile_inheritance_limits_merged(coi_binary, cleanup_containers, workspace_dir):
    """Child overrides one limit, parent's other limits kept."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[limits.cpu]\ncount = "4"\n\n[limits.memory]\nlimit = "2GiB"\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n\n[limits.memory]\nlimit = "4GiB"\n'
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
    assert "4" in output, f"Should have CPU count from parent. Got:\n{output}"
    assert "4GiB" in output, f"Should have memory limit from child. Got:\n{output}"


def test_profile_inheritance_tool_merged(coi_binary, cleanup_containers, workspace_dir):
    """Child overrides tool name, parent's permission_mode kept."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[tool]\nname = "claude"\npermission_mode = "bypass"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text('inherits = "parent"\n\n[tool]\nname = "aider"\n')

    result = subprocess.run(
        [coi_binary, "profile", "show", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "aider" in output, f"Should have child tool name. Got:\n{output}"
    assert "bypass" in output, f"Should inherit parent permission_mode. Got:\n{output}"


def test_profile_inheritance_network_inherited(coi_binary, cleanup_containers, workspace_dir):
    """Child without [network] gets parent's network."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[network]\nmode = "restricted"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text('inherits = "parent"\nimage = "coi-default"\n')

    result = subprocess.run(
        [coi_binary, "profile", "show", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "restricted" in output, f"Should inherit parent network. Got:\n{output}"
