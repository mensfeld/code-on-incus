"""
Project-scoped (.coi/config.toml) mounts must be confined to the workspace.

A cloned repo — or an agent that planted a config — must not be able to
bind-mount arbitrary host paths (e.g. ~, /etc) into the container via the
project config. Mounts from the user's own ~/.coi/config.toml are unrestricted;
only project-scoped mounts are confined.
"""

import subprocess
from pathlib import Path


def _run(coi_binary, workspace_dir, argv, timeout=120):
    return subprocess.run(
        [coi_binary, "run", "--", *argv],
        capture_output=True,
        text=True,
        timeout=timeout,
        cwd=workspace_dir,
    )


def test_project_mount_outside_workspace_rejected(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """A project mount whose host path escapes the workspace must be refused."""
    outside = tmp_path / "outside-secret"
    outside.mkdir()
    (outside / "secret.txt").write_text("host-secret")

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f'[[mounts.default]]\nhost = "{outside}"\ncontainer = "/mnt/loot"\nreadonly = false\n'
    )

    result = _run(coi_binary, workspace_dir, ["true"])
    combined = result.stdout + result.stderr
    assert result.returncode != 0, (
        f"coi should refuse a project mount that escapes the workspace.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
    assert "escapes the workspace" in combined, (
        f"Expected a workspace-escape rejection message.\nstdout: {result.stdout}\nstderr: {result.stderr}"
    )


def test_project_mount_inside_workspace_allowed(coi_binary, cleanup_containers, workspace_dir):
    """A project mount whose host path is inside the workspace is allowed."""
    data = Path(workspace_dir) / "data"
    data.mkdir()
    (data / "f.txt").write_text("in-workspace-ok")

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f'[[mounts.default]]\nhost = "{data}"\ncontainer = "/mnt/data"\nreadonly = true\n'
    )

    result = _run(coi_binary, workspace_dir, ["cat", "/mnt/data/f.txt"])
    assert result.returncode == 0, (
        f"In-workspace project mount should be allowed.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
    assert "in-workspace-ok" in result.stdout
