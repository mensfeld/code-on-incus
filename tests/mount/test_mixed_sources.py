"""Test mixing config mounts, --storage, and --mount."""

import subprocess
from pathlib import Path


def test_config_storage_and_mount(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test combining all three mount sources."""
    config_dir = tmp_path / "config-mount"
    storage_dir = tmp_path / "storage"
    cli_dir = tmp_path / "cli-mount"

    for d in [config_dir, storage_dir, cli_dir]:
        d.mkdir()

    (config_dir / "fc.txt").write_text("from-config")
    (storage_dir / "fs.txt").write_text("from-storage")
    (cli_dir / "fm.txt").write_text("from-cli")

    # Create config file in workspace directory
    config_content = f"""
[[mounts.default]]
host = "{config_dir}"
container = "/config-data"
"""
    config_file = Path(workspace_dir) / ".coi.toml"
    config_file.write_text(config_content)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--storage",
            str(storage_dir),
            "--mount",
            f"{cli_dir}:/cli-data",
            "--",
            "sh",
            "-c",
            "cat /config-data/fc.txt && cat /storage/fs.txt && cat /cli-data/fm.txt",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "from-config" in result.stdout
    assert "from-storage" in result.stdout
    assert "from-cli" in result.stdout
