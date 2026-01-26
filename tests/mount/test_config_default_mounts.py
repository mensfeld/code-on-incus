"""Test default mounts from config file."""
import subprocess
from pathlib import Path
import tempfile

def test_config_default_mounts(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test that config file default mounts are applied."""
    # Create mount directories
    mount1 = tmp_path / "mount1"
    mount2 = tmp_path / "mount2"
    mount1.mkdir()
    mount2.mkdir()
    (mount1 / "file1.txt").write_text("content1")
    (mount2 / "file2.txt").write_text("content2")

    # Create config file in workspace directory (.coi.toml)
    config_content = f"""
[mounts]
[[mounts.default]]
host = "{mount1}"
container = "/mnt/data1"

[[mounts.default]]
host = "{mount2}"
container = "/mnt/data2"
"""

    config_file = Path(workspace_dir) / ".coi.toml"
    config_file.write_text(config_content)

    # Run - config file will be loaded automatically
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir,
         "--", "sh", "-c", "cat /mnt/data1/file1.txt && cat /mnt/data2/file2.txt"],
        capture_output=True, text=True, timeout=120
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "content1" in result.stdout
    assert "content2" in result.stdout

def test_cli_overrides_config_mount(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test that CLI --mount overrides config mount for same container path."""
    config_mount = tmp_path / "config-data"
    cli_mount = tmp_path / "cli-data"
    config_mount.mkdir()
    cli_mount.mkdir()
    (config_mount / "file.txt").write_text("from-config")
    (cli_mount / "file.txt").write_text("from-cli")

    # Create config file in workspace directory
    config_content = f"""
[[mounts.default]]
host = "{config_mount}"
container = "/data"
"""
    config_file = Path(workspace_dir) / ".coi.toml"
    config_file.write_text(config_content)

    # CLI also mounts to /data (should override)
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir,
         "--mount", f"{cli_mount}:/data",
         "--", "cat", "/data/file.txt"],
        capture_output=True, text=True, timeout=120
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "from-cli" in result.stdout
    assert "from-config" not in result.stdout
