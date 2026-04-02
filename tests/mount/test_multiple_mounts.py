"""Test multiple mount entries via config file."""

import subprocess
from pathlib import Path


def test_multiple_config_mounts(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test mounting multiple directories via config file."""
    dir1 = tmp_path / "data1"
    dir2 = tmp_path / "data2"
    dir3 = tmp_path / "data3"

    for d in [dir1, dir2, dir3]:
        d.mkdir()

    (dir1 / "f1.txt").write_text("content1")
    (dir2 / "f2.txt").write_text("content2")
    (dir3 / "f3.txt").write_text("content3")

    # Create config file in workspace directory (.coi/config.toml)
    config_content = f"""\
[[mounts.default]]
host = "{dir1}"
container = "/d1"

[[mounts.default]]
host = "{dir2}"
container = "/d2"

[[mounts.default]]
host = "{dir3}"
container = "/d3"
"""
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(config_content)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--",
            "sh",
            "-c",
            "cat /d1/f1.txt && cat /d2/f2.txt && cat /d3/f3.txt",
        ],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "content1" in result.stdout
    assert "content2" in result.stdout
    assert "content3" in result.stdout
