"""Test multiple --mount flags."""

import subprocess


def test_multiple_cli_mounts(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test mounting multiple directories via CLI."""
    dir1 = tmp_path / "data1"
    dir2 = tmp_path / "data2"
    dir3 = tmp_path / "data3"

    for d in [dir1, dir2, dir3]:
        d.mkdir()

    (dir1 / "f1.txt").write_text("content1")
    (dir2 / "f2.txt").write_text("content2")
    (dir3 / "f3.txt").write_text("content3")

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--mount",
            f"{dir1}:/d1",
            "--mount",
            f"{dir2}:/d2",
            "--mount",
            f"{dir3}:/d3",
            "--",
            "sh",
            "-c",
            "cat /d1/f1.txt && cat /d2/f2.txt && cat /d3/f3.txt",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "content1" in result.stdout
    assert "content2" in result.stdout
    assert "content3" in result.stdout
