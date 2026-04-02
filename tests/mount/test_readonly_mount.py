"""Test readonly mount support via config file."""

import subprocess
from pathlib import Path


def test_readonly_mount_readable(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test that a readonly mount allows reading files inside the container."""
    data_dir = tmp_path / "rodata"
    data_dir.mkdir()
    (data_dir / "secret.txt").write_text("readonly-content")

    # Create config file with readonly mount
    config_content = f"""\
[[mounts.default]]
host = "{data_dir}"
container = "/mnt/rodata"
readonly = true
"""
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(config_content)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--",
            "cat",
            "/mnt/rodata/secret.txt",
        ],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "readonly-content" in result.stdout


def test_readonly_mount_write_fails(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test that writing to a readonly mount fails inside the container."""
    data_dir = tmp_path / "rodata"
    data_dir.mkdir()
    (data_dir / "existing.txt").write_text("do-not-modify")

    # Create config file with readonly mount
    config_content = f"""\
[[mounts.default]]
host = "{data_dir}"
container = "/mnt/rodata"
readonly = true
"""
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(config_content)

    # First, verify the mount is present and readable (guards against false-pass
    # if the mount was never applied)
    read_result = subprocess.run(
        [coi_binary, "run", "--", "cat", "/mnt/rodata/existing.txt"],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )
    assert read_result.returncode == 0, (
        f"Mount should be present and readable.\n"
        f"stdout: {read_result.stdout}\nstderr: {read_result.stderr}"
    )
    assert "do-not-modify" in read_result.stdout

    # Now attempt to write a new file in the readonly mount
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--",
            "sh",
            "-c",
            "echo 'should-fail' > /mnt/rodata/newfile.txt",
        ],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )

    # The write should fail (non-zero exit or "Read-only" error message)
    combined = result.stdout + result.stderr
    assert result.returncode != 0 or "read-only" in combined.lower(), (
        f"Write to readonly mount should have failed.\n"
        f"returncode: {result.returncode}\n"
        f"stdout: {result.stdout}\n"
        f"stderr: {result.stderr}"
    )

    # Verify the original file was not modified on the host
    assert (data_dir / "existing.txt").read_text() == "do-not-modify"
    # Verify the new file was not created on the host
    assert not (data_dir / "newfile.txt").exists()
