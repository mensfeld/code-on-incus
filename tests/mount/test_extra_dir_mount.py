"""Test mounting extra directories via config file (e.g., config dirs, agents/)."""

import os
import subprocess
import tempfile
import time
from pathlib import Path

from support.helpers import calculate_container_name


def test_extra_dir_mount_readable(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """Test that an extra directory with nested subdirectories is readable inside the container.

    Simulates mounting a config directory (like opencode agents/) into a container
    and verifying all files including nested ones are accessible.
    """
    config_dir = tmp_path / "myconfig"
    agents_dir = config_dir / "agents"
    agents_dir.mkdir(parents=True)

    (config_dir / "AGENTS.md").write_text("# Top-level agents file")
    (agents_dir / "review.md").write_text("Review agent config")
    (agents_dir / "plan.md").write_text("Plan agent config")

    # Create config file in workspace directory (.coi/config.toml)
    mount_config = f"""\
[[mounts.default]]
host = "{config_dir}"
container = "/home/code/.config/custom"
"""
    coi_dir = Path(workspace_dir) / ".coi"
    coi_dir.mkdir(exist_ok=True)
    (coi_dir / "config.toml").write_text(mount_config)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--",
            "sh",
            "-c",
            "cat /home/code/.config/custom/AGENTS.md"
            " && cat /home/code/.config/custom/agents/review.md"
            " && cat /home/code/.config/custom/agents/plan.md",
        ],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "# Top-level agents file" in result.stdout
    assert "Review agent config" in result.stdout
    assert "Plan agent config" in result.stdout


def test_extra_dir_mount_bidirectional(coi_binary, cleanup_containers, workspace_dir):
    """Test that files written inside the container appear on the host and vice versa.

    Uses container launch + mount + exec pattern to verify bidirectional access.
    """
    container_name = calculate_container_name(workspace_dir, 1)

    with tempfile.TemporaryDirectory() as tmpdir:
        # Create a file on the host
        host_file = os.path.join(tmpdir, "from-host.txt")
        with open(host_file, "w") as f:
            f.write("hello-from-host")

        # Launch container
        result = subprocess.run(
            [coi_binary, "container", "launch", "coi", container_name],
            capture_output=True,
            text=True,
            timeout=120,
        )
        assert result.returncode == 0, f"Container launch failed. stderr: {result.stderr}"

        time.sleep(3)

        # Mount the directory
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "mount",
                container_name,
                "extra-config",
                tmpdir,
                "/mnt/extra",
            ],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode == 0, f"Mount failed. stderr: {result.stderr}"

        time.sleep(2)

        # Verify host file is readable inside the container
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "cat",
                "/mnt/extra/from-host.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert result.returncode == 0, f"Reading host file failed. stderr: {result.stderr}"
        combined = result.stdout + result.stderr
        assert "hello-from-host" in combined, f"Host file content not found. Got:\n{combined}"

        # Create a file inside the container
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "echo 'hello-from-container' > /mnt/extra/from-container.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert result.returncode == 0, f"Creating file in container failed. stderr: {result.stderr}"

        time.sleep(1)

        # Verify the file appears on the host
        container_file = os.path.join(tmpdir, "from-container.txt")
        assert os.path.exists(container_file), (
            f"File created in container not found on host. Dir contents: {os.listdir(tmpdir)}"
        )
        with open(container_file) as f:
            content = f.read().strip()
        assert content == "hello-from-container", (
            f"Unexpected content from container file: {content!r}"
        )

        # Cleanup
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
