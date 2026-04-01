"""
Test coi profile list command.

Tests that:
1. Lists profiles from inline config
2. Lists profiles from directory config
3. Shows correct source information
4. Works when no profiles are defined
"""

import subprocess
from pathlib import Path


def test_profile_list_inline(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi profile list shows inline profiles.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(parents=True)
    (config_dir / "config.toml").write_text(
        """
[profiles.rust]
image = "coi-rust"
persistent = true

[profiles.python]
image = "coi-python"
"""
    )

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

    assert result.returncode == 0, f"profile list should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "rust" in output, f"Should list 'rust' profile. Got:\n{output}"
    assert "python" in output, f"Should list 'python' profile. Got:\n{output}"
    assert "coi-rust" in output, f"Should show image for rust profile. Got:\n{output}"


def test_profile_list_directory(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi profile list shows profiles from directories.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "dirprof"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('image = "coi-dir"\n')

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

    assert result.returncode == 0, f"profile list should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "dirprof" in output, f"Should list directory profile. Got:\n{output}"
    assert "coi-dir" in output, f"Should show image for dir profile. Got:\n{output}"


def test_profile_list_empty(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi profile list handles no profiles gracefully.
    """
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

    assert result.returncode == 0, f"profile list should succeed even with no profiles. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "no profiles" in output.lower(), (
        f"Should indicate no profiles configured. Got:\n{output}"
    )


def test_profile_list_mixed_sources(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi profile list shows both inline and directory profiles.
    """
    # Create directory profile
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "from-dir"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('image = "dir-img"\n')

    # Create inline profile
    config_dir = Path(workspace_dir) / ".coi"
    (config_dir / "config.toml").write_text(
        """
[profiles.from-inline]
image = "inline-img"
"""
    )

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

    assert result.returncode == 0, f"profile list should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "from-dir" in output, f"Should list directory profile. Got:\n{output}"
    assert "from-inline" in output, f"Should list inline profile. Got:\n{output}"
