"""
Test profile inheritance error handling and chains.

Tests chains (A inherits B inherits C), missing parent errors,
cycle detection, self-cycles, and cross-level inheritance.
"""

import subprocess
from pathlib import Path


def test_profile_inheritance_chain(coi_binary, cleanup_containers, workspace_dir):
    """A inherits B inherits C, all settings resolve correctly."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    gp_dir = coi_dir / "grandparent"
    gp_dir.mkdir(parents=True)
    (gp_dir / "config.toml").write_text(
        '[environment]\nLEVEL = "gp"\nGP_ONLY = "yes"\n\n[container]\nimage = "coi-gp"\n'
    )

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        'inherits = "grandparent"\n\n[environment]\nLEVEL = "parent"\n\n[container]\nimage = "coi-parent"\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n\n[environment]\nLEVEL = "child"\n'
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
    assert "coi-parent" in output, f"Image from parent. Got:\n{output}"
    assert "GP_ONLY" in output, f"GP_ONLY from grandparent. Got:\n{output}"


def test_profile_inheritance_missing_parent_fails(coi_binary, cleanup_containers, workspace_dir):
    """Error when parent profile doesn't exist."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text('inherits = "nonexistent"\n')

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Should fail when parent not found"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), f"Should mention not found. Got:\n{combined}"


def test_profile_inheritance_cycle_fails(coi_binary, cleanup_containers, workspace_dir):
    """A -> B -> A produces clear error."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    a_dir = coi_dir / "a"
    a_dir.mkdir(parents=True)
    (a_dir / "config.toml").write_text('inherits = "b"\n')

    b_dir = coi_dir / "b"
    b_dir.mkdir(parents=True)
    (b_dir / "config.toml").write_text('inherits = "a"\n')

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Should fail on cycle"
    combined = result.stdout + result.stderr
    assert "cycle" in combined.lower(), f"Should mention cycle. Got:\n{combined}"


def test_profile_inheritance_self_cycle_fails(coi_binary, cleanup_containers, workspace_dir):
    """A -> A produces clear error."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    a_dir = coi_dir / "a"
    a_dir.mkdir(parents=True)
    (a_dir / "config.toml").write_text('inherits = "a"\n')

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Should fail on self-cycle"
    combined = result.stdout + result.stderr
    assert "cycle" in combined.lower(), f"Should mention cycle. Got:\n{combined}"


def test_profile_inheritance_cross_level(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Project-level profile inherits from user-level profile."""
    # Create a user-level config dir with a parent profile
    user_config_dir = tmp_path / "user_config"
    user_profile_dir = user_config_dir / "profiles" / "base-rust"
    user_profile_dir.mkdir(parents=True)
    (user_profile_dir / "config.toml").write_text(
        'forward_env = ["RUST_BACKTRACE"]\n\n[environment]\nEDITOR = "vim"\n\n[container]\nimage = "coi-rust"\n'
    )

    # Create a project-level child profile
    proj_profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "my-rust"
    proj_profile_dir.mkdir(parents=True)
    (proj_profile_dir / "config.toml").write_text(
        'inherits = "base-rust"\n\n[environment]\nMY_VAR = "hello"\n\n[container]\nimage = "coi-rust-custom"\n'
    )

    # Create user config.toml (needed for config loading)
    (user_config_dir / "config.toml").write_text("")

    # Use COI_CONFIG to load user config dir
    result = subprocess.run(
        [coi_binary, "profile", "info", "my-rust", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
        env={
            **dict(__import__("os").environ),
            "COI_CONFIG": str(user_config_dir / "config.toml"),
        },
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-rust-custom" in output, f"Should have child image. Got:\n{output}"
    assert "EDITOR" in output, f"Should inherit parent EDITOR. Got:\n{output}"
    assert "MY_VAR" in output, f"Should have child MY_VAR. Got:\n{output}"
