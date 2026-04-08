"""
Tests for profile create, edit, and delete commands (Issue #114, Phase 2).
"""

import os
import subprocess


def test_profile_create_basic(coi_binary, tmp_path):
    """coi profile create with --image should create a profile and write its config."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    result = subprocess.run(
        [coi_binary, "profile", "create", "test-basic", "--image", "coi-test", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env={**os.environ, "HOME": str(tmp_path)},
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    assert "Created profile 'test-basic'" in result.stderr

    config_path = home_coi / "profiles" / "test-basic" / "config.toml"
    assert config_path.exists(), f"config.toml not created at {config_path}"

    content = config_path.read_text()
    assert 'image = "coi-test"' in content


def test_profile_create_with_inherits(coi_binary, tmp_path):
    """coi profile create with --inherits should set the inherits field."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "create",
            "test-inherit",
            "--image",
            "coi-test",
            "--inherits",
            "default",
            "--user",
        ],
        capture_output=True,
        text=True,
        timeout=30,
        env={**os.environ, "HOME": str(tmp_path)},
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"

    config_path = home_coi / "profiles" / "test-inherit" / "config.toml"
    content = config_path.read_text()
    assert 'inherits = "default"' in content
    assert 'image = "coi-test"' in content


def test_profile_create_with_persistent(coi_binary, tmp_path):
    """coi profile create with --persistent should set persistent = true."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    result = subprocess.run(
        [coi_binary, "profile", "create", "test-persist", "--persistent", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env={**os.environ, "HOME": str(tmp_path)},
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"

    config_path = home_coi / "profiles" / "test-persist" / "config.toml"
    content = config_path.read_text()
    assert "persistent = true" in content


def test_profile_create_duplicate_fails(coi_binary, tmp_path):
    """Creating a profile with the same name twice should fail."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    env = {**os.environ, "HOME": str(tmp_path)}

    # First create succeeds
    result = subprocess.run(
        [coi_binary, "profile", "create", "test-dup", "--image", "img", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode == 0

    # Second create fails
    result = subprocess.run(
        [coi_binary, "profile", "create", "test-dup", "--image", "img2", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode != 0
    assert "already exists" in result.stderr


def test_profile_create_default_name_rejected(coi_binary, tmp_path):
    """Creating a profile named 'default' should be rejected."""
    result = subprocess.run(
        [coi_binary, "profile", "create", "default", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env={**os.environ, "HOME": str(tmp_path)},
    )

    assert result.returncode != 0
    assert "default" in result.stderr.lower()


def test_profile_create_project_flag(coi_binary, tmp_path):
    """--project should create the profile in .coi/profiles/ under the workspace."""
    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "create",
            "test-proj",
            "--image",
            "coi-test",
            "--project",
            "--workspace",
            str(tmp_path),
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"

    config_path = tmp_path / ".coi" / "profiles" / "test-proj" / "config.toml"
    assert config_path.exists(), f"config.toml not created at {config_path}"


def test_profile_create_dot_names_rejected(coi_binary, tmp_path):
    """Profile names like '.' and '..' should be rejected (path traversal)."""
    for dot_name in [".", "..", "..."]:
        result = subprocess.run(
            [coi_binary, "profile", "create", dot_name, "--user"],
            capture_output=True,
            text=True,
            timeout=30,
            env={**os.environ, "HOME": str(tmp_path)},
        )

        assert result.returncode != 0, f"Name '{dot_name}' should be rejected"
        assert "dots" in result.stderr.lower() or "cannot be" in result.stderr.lower()


def test_profile_create_slash_in_name_rejected(coi_binary, tmp_path):
    """Profile names with slashes should be rejected."""
    result = subprocess.run(
        [coi_binary, "profile", "create", "bad/name", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env={**os.environ, "HOME": str(tmp_path)},
    )

    assert result.returncode != 0
    assert "slash" in result.stderr.lower()


def test_profile_create_user_and_project_mutually_exclusive(coi_binary, tmp_path):
    """--user and --project together should fail."""
    result = subprocess.run(
        [coi_binary, "profile", "create", "test-both", "--user", "--project"],
        capture_output=True,
        text=True,
        timeout=30,
        env={**os.environ, "HOME": str(tmp_path)},
    )

    assert result.returncode != 0
    assert "mutually exclusive" in result.stderr.lower()


def test_profile_delete_basic(coi_binary, tmp_path):
    """Create then delete with --force should remove the profile."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    env = {**os.environ, "HOME": str(tmp_path)}

    # Create
    result = subprocess.run(
        [coi_binary, "profile", "create", "test-del", "--image", "img", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode == 0

    profile_dir = home_coi / "profiles" / "test-del"
    assert profile_dir.exists()

    # Delete
    result = subprocess.run(
        [coi_binary, "profile", "delete", "test-del", "--force"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    assert "Deleted profile 'test-del'" in result.stderr
    assert not profile_dir.exists()


def test_profile_delete_nonexistent_fails(coi_binary):
    """Deleting a profile that doesn't exist should fail."""
    result = subprocess.run(
        [coi_binary, "profile", "delete", "no-such-profile", "--force"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0
    assert "not found" in result.stderr


def test_profile_delete_builtin_fails(coi_binary):
    """Deleting the built-in 'default' profile should fail."""
    result = subprocess.run(
        [coi_binary, "profile", "delete", "default", "--force"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0
    assert "built-in" in result.stderr.lower()


def test_profile_edit_nonexistent_fails(coi_binary):
    """Editing a profile that doesn't exist should fail."""
    result = subprocess.run(
        [coi_binary, "profile", "edit", "no-such-profile"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0
    assert "not found" in result.stderr


def test_profile_edit_builtin_fails(coi_binary):
    """Editing the built-in 'default' profile should fail."""
    result = subprocess.run(
        [coi_binary, "profile", "edit", "default"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0
    assert "built-in" in result.stderr.lower()


def test_profile_create_visible_in_list(coi_binary, tmp_path):
    """A newly created profile should appear in 'profile list'."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    env = {**os.environ, "HOME": str(tmp_path)}

    result = subprocess.run(
        [coi_binary, "profile", "create", "list-test", "--image", "coi-test", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode == 0

    result = subprocess.run(
        [coi_binary, "profile", "list"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode == 0
    assert "list-test" in result.stdout


def test_profile_create_no_flags_produces_empty_config(coi_binary, tmp_path):
    """Creating a profile with no --image/--inherits/--persistent produces an empty config."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    result = subprocess.run(
        [coi_binary, "profile", "create", "bare-profile", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env={**os.environ, "HOME": str(tmp_path)},
    )
    assert result.returncode == 0

    config_path = home_coi / "profiles" / "bare-profile" / "config.toml"
    assert config_path.exists()
    assert config_path.read_text() == ""


def test_profile_create_auto_detects_project(coi_binary, tmp_path):
    """Without --user/--project, create should use project .coi/ if it exists."""
    # Set up a project with .coi/ directory
    project_coi = tmp_path / ".coi"
    project_coi.mkdir()

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "create",
            "auto-proj",
            "--image",
            "img",
            "--workspace",
            str(tmp_path),
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"

    config_path = project_coi / "profiles" / "auto-proj" / "config.toml"
    assert config_path.exists(), f"Expected profile at project location {config_path}"


def test_profile_create_duplicate_across_locations_fails(coi_binary, tmp_path):
    """Creating a profile name that already exists in another location should fail."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    env = {**os.environ, "HOME": str(tmp_path)}

    # Create at user level
    result = subprocess.run(
        [coi_binary, "profile", "create", "cross-dup", "--image", "img", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode == 0

    # Try to create same name at project level — should fail because name exists in cfg
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    (project_dir / ".coi").mkdir()

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "create",
            "cross-dup",
            "--image",
            "img2",
            "--project",
            "--workspace",
            str(project_dir),
        ],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode != 0
    assert "already exists" in result.stderr


def test_profile_edit_with_true_editor(coi_binary, tmp_path):
    """Edit with EDITOR=true should succeed (true exits 0 without modifying)."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    env = {**os.environ, "HOME": str(tmp_path)}

    # Create a profile first
    result = subprocess.run(
        [coi_binary, "profile", "create", "edit-test", "--image", "img", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode == 0

    # Edit with EDITOR=true (no-op editor that exits 0)
    result = subprocess.run(
        [coi_binary, "profile", "edit", "edit-test"],
        capture_output=True,
        text=True,
        timeout=30,
        env={**env, "EDITOR": "true", "VISUAL": ""},
    )
    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"


def test_profile_delete_short_flag(coi_binary, tmp_path):
    """The -f short flag should work the same as --force."""
    home_coi = tmp_path / ".coi"
    home_coi.mkdir()

    env = {**os.environ, "HOME": str(tmp_path)}

    # Create
    result = subprocess.run(
        [coi_binary, "profile", "create", "test-sf", "--image", "img", "--user"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode == 0

    # Delete with -f
    result = subprocess.run(
        [coi_binary, "profile", "delete", "test-sf", "-f"],
        capture_output=True,
        text=True,
        timeout=30,
        env=env,
    )
    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    assert "Deleted profile 'test-sf'" in result.stderr
