"""Test backward compatibility of --storage flag."""

import subprocess


def test_storage_flag_still_works(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Verify --storage flag works as before."""
    storage_dir = tmp_path / "storage"
    storage_dir.mkdir()
    (storage_dir / "data.txt").write_text("storage-content")

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--storage",
            str(storage_dir),
            "--",
            "cat",
            "/storage/data.txt",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "storage-content" in result.stdout
