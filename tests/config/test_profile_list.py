"""
Test coi profile list command.

Tests that:
1. Lists profiles from directory config
2. Shows correct source information
3. Works when no profiles are defined
4. Lists multiple profiles from directories
"""

import subprocess
from pathlib import Path


def test_profile_list_directory(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi profile list shows profiles from directories.
    """
    for name, image in [("rust", "coi-rust"), ("python", "coi-python")]:
        profile_dir = Path(workspace_dir) / ".coi" / "profiles" / name
        profile_dir.mkdir(parents=True)
        (profile_dir / "config.toml").write_text(f'image = "{image}"\n')

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


def test_profile_list_shows_default(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi profile list always shows the built-in default profile,
    even when no user profiles are defined.
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

    assert result.returncode == 0, f"profile list should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "default" in output, f"Should show built-in 'default' profile. Got:\n{output}"
    assert "(built-in)" in output, (
        f"Should show '(built-in)' as source for default profile. Got:\n{output}"
    )


def test_profile_list_multiple_directories(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi profile list shows multiple directory profiles.
    """
    for name, image in [("from-dir-a", "dir-img-a"), ("from-dir-b", "dir-img-b")]:
        profile_dir = Path(workspace_dir) / ".coi" / "profiles" / name
        profile_dir.mkdir(parents=True)
        (profile_dir / "config.toml").write_text(f'image = "{image}"\n')

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
    assert "from-dir-a" in output, f"Should list first directory profile. Got:\n{output}"
    assert "from-dir-b" in output, f"Should list second directory profile. Got:\n{output}"
