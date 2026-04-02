"""Test default mounts from config file."""

import subprocess
from pathlib import Path


def test_config_default_mounts(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test that config file default mounts are applied."""
    # Create mount directories
    mount1 = tmp_path / "mount1"
    mount2 = tmp_path / "mount2"
    mount1.mkdir()
    mount2.mkdir()
    (mount1 / "file1.txt").write_text("content1")
    (mount2 / "file2.txt").write_text("content2")

    # Create config file in workspace directory (.coi/config.toml)
    config_content = f"""\
[[mounts.default]]
host = "{mount1}"
container = "/mnt/data1"

[[mounts.default]]
host = "{mount2}"
container = "/mnt/data2"
"""

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_file = config_dir / "config.toml"
    config_file.write_text(config_content)

    # Run from workspace directory so config is loaded
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--",
            "sh",
            "-c",
            "cat /mnt/data1/file1.txt && cat /mnt/data2/file2.txt",
        ],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,  # Run from workspace directory to load .coi/config.toml
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "content1" in result.stdout
    assert "content2" in result.stdout


def test_config_mount_replaces_previous(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test that updating config mount entries works correctly."""
    mount1 = tmp_path / "first-data"
    mount2 = tmp_path / "second-data"
    mount1.mkdir()
    mount2.mkdir()
    (mount1 / "file.txt").write_text("from-first")
    (mount2 / "file.txt").write_text("from-second")

    # Create config file with only the second mount at /data
    config_content = f"""\
[[mounts.default]]
host = "{mount2}"
container = "/data"
"""
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_file = config_dir / "config.toml"
    config_file.write_text(config_content)

    result = subprocess.run(
        [coi_binary, "run", "--", "cat", "/data/file.txt"],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "from-second" in result.stdout
    assert "from-first" not in result.stdout
