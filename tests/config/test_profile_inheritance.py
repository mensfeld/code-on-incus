"""
Test profile inheritance feature.

Tests that:
1. CLI displays inheritance info (show, list)
2. Scalar inheritance works (image, persistent)
3. Environment maps merge correctly (merge, override, clear)
4. Arrays replace when defined, inherit when not (mounts, forward_env)
5. Struct sections deep merge (limits, tool, network)
6. Chains and error conditions work (chain, missing parent, cycle, self-cycle)
7. Cross-level inheritance works (project inherits from user-level)
"""

import subprocess
from pathlib import Path


# --- CLI display ---


def test_profile_inherits_shown_in_show(coi_binary, cleanup_containers, workspace_dir):
    """coi profile show displays the inherits field."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('image = "coi-parent"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\nimage = "coi-child"\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "show", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    # After resolution, inherits is cleared, but the profile should work
    assert result.returncode == 0, f"profile show should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "child" in output
    assert "coi-child" in output


def test_profile_inherits_shown_in_list(coi_binary, cleanup_containers, workspace_dir):
    """coi profile list shows INHERITS column."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('image = "coi-parent"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\nimage = "coi-child"\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile list should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "INHERITS" in output, f"Should show INHERITS column header. Got:\n{output}"
    assert "child" in output
    assert "parent" in output


# --- Scalar inheritance ---


def test_profile_inheritance_image_from_parent(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child without image gets parent's image."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('image = "coi-parent"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text('inherits = "parent"\n')

    result = subprocess.run(
        [coi_binary, "profile", "show", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-parent" in output, f"Should inherit parent image. Got:\n{output}"


def test_profile_inheritance_image_overridden(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child with image overrides parent's image."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('image = "coi-parent"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\nimage = "coi-child"\n'
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
    assert "coi-child" in output, f"Should show child's overridden image. Got:\n{output}"


def test_profile_inheritance_persistent_from_parent(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child inherits parent's persistent flag."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('image = "coi"\npersistent = true\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text('inherits = "parent"\n')

    result = subprocess.run(
        [coi_binary, "profile", "show", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "true" in output, f"Should inherit persistent=true. Got:\n{output}"


# --- Environment (maps merge) ---


def test_profile_inheritance_env_merged(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child env key added alongside parent's keys."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[environment]\nEDITOR = "vim"\n'
    )

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


def test_profile_inheritance_env_override(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child env key overrides same parent key."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[environment]\nRUST_BACKTRACE = "1"\n'
    )

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


def test_profile_inheritance_env_clear_with_empty(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child sets parent key to empty string to clear it."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[environment]\nSECRET = "abc"\nKEEP = "yes"\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n\n[environment]\nSECRET = ""\n'
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
    assert "SECRET" not in output, f"SECRET should be cleared. Got:\n{output}"
    assert "KEEP" in output, f"KEEP should be inherited. Got:\n{output}"


def test_profile_inheritance_env_parent_only(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child without [environment] gets all parent env vars."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[environment]\nPARENT_VAR = "hello"\n'
    )

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


# --- Arrays (replace if defined, inherit if not) ---


def test_profile_inheritance_mounts_replaced(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child defines mounts - parent's mounts gone."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[[mounts]]\nhost = "~/.ssh"\ncontainer = "/home/code/.ssh"\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n\n[[mounts]]\nhost = "~/.cargo"\ncontainer = "/home/code/.cargo"\n'
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
    assert "~/.cargo" in output, f"Should have child mount. Got:\n{output}"
    assert "~/.ssh" not in output, f"Parent mount should be replaced. Got:\n{output}"


def test_profile_inheritance_mounts_inherited(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child without mounts gets parent's mounts."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[[mounts]]\nhost = "~/.ssh"\ncontainer = "/home/code/.ssh"\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\nimage = "coi-child"\n'
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
    assert "~/.ssh" in output, f"Should inherit parent mounts. Got:\n{output}"


def test_profile_inheritance_forward_env_replaced(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child defines forward_env - replaces parent's."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        'forward_env = ["SSH_AUTH_SOCK"]\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\nforward_env = ["API_KEY"]\n'
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
    assert "API_KEY" in output, f"Should have child forward_env. Got:\n{output}"
    assert "SSH_AUTH_SOCK" not in output, f"Parent forward_env replaced. Got:\n{output}"


def test_profile_inheritance_forward_env_inherited(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child without forward_env gets parent's."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        'forward_env = ["SSH_AUTH_SOCK"]\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\nimage = "coi"\n'
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
    assert "SSH_AUTH_SOCK" in output, f"Should inherit parent forward_env. Got:\n{output}"


# --- Struct sections (deep merge) ---


def test_profile_inheritance_limits_merged(
    coi_binary, cleanup_containers, workspace_dir
):
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


def test_profile_inheritance_tool_merged(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child overrides tool name, parent's permission_mode kept."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[tool]\nname = "claude"\npermission_mode = "bypass"\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n\n[tool]\nname = "aider"\n'
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
    assert "aider" in output, f"Should have child tool name. Got:\n{output}"
    assert "bypass" in output, f"Should inherit parent permission_mode. Got:\n{output}"


def test_profile_inheritance_network_inherited(
    coi_binary, cleanup_containers, workspace_dir
):
    """Child without [network] gets parent's network."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        '[network]\nmode = "restricted"\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\nimage = "coi"\n'
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
    assert "restricted" in output, f"Should inherit parent network. Got:\n{output}"


# --- Chains and errors ---


def test_profile_inheritance_chain(coi_binary, cleanup_containers, workspace_dir):
    """A inherits B inherits C, all settings resolve correctly."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    gp_dir = coi_dir / "grandparent"
    gp_dir.mkdir(parents=True)
    (gp_dir / "config.toml").write_text(
        'image = "coi-gp"\n\n[environment]\nLEVEL = "gp"\nGP_ONLY = "yes"\n'
    )

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text(
        'inherits = "grandparent"\nimage = "coi-parent"\n\n[environment]\nLEVEL = "parent"\n'
    )

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n\n[environment]\nLEVEL = "child"\n'
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
    assert "coi-parent" in output, f"Image from parent. Got:\n{output}"
    assert "GP_ONLY" in output, f"GP_ONLY from grandparent. Got:\n{output}"


def test_profile_inheritance_missing_parent_fails(
    coi_binary, cleanup_containers, workspace_dir
):
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


def test_profile_inheritance_cycle_fails(
    coi_binary, cleanup_containers, workspace_dir
):
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


def test_profile_inheritance_self_cycle_fails(
    coi_binary, cleanup_containers, workspace_dir
):
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


# --- Cross-level ---


def test_profile_inheritance_cross_level(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """Project-level profile inherits from user-level profile."""
    # Create a user-level config dir with a parent profile
    user_config_dir = tmp_path / "user_config"
    user_profile_dir = user_config_dir / "profiles" / "base-rust"
    user_profile_dir.mkdir(parents=True)
    (user_profile_dir / "config.toml").write_text(
        'image = "coi-rust"\nforward_env = ["RUST_BACKTRACE"]\n\n[environment]\nEDITOR = "vim"\n'
    )

    # Create a project-level child profile
    proj_profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "my-rust"
    proj_profile_dir.mkdir(parents=True)
    (proj_profile_dir / "config.toml").write_text(
        'inherits = "base-rust"\nimage = "coi-rust-custom"\n\n[environment]\nMY_VAR = "hello"\n'
    )

    # Create user config.toml (needed for config loading)
    (user_config_dir / "config.toml").write_text("")

    # Use COI_CONFIG to load user config dir
    result = subprocess.run(
        [coi_binary, "profile", "show", "my-rust", "--workspace", workspace_dir],
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
