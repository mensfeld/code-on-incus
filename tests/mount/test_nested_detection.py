"""Test nested mount path detection."""

import subprocess
from pathlib import Path


def test_nested_mounts_rejected(coi_binary, workspace_dir, tmp_path):
    """Test that nested paths fail validation."""
    dir1 = tmp_path / "data"
    dir2 = tmp_path / "other"
    dir1.mkdir()
    dir2.mkdir()

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--mount",
            f"{dir1}:/data",
            "--mount",
            f"{dir2}:/data/nested",  # Nested!
            "--",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode != 0
    combined = result.stdout + result.stderr
    assert "nested" in combined.lower() or "conflict" in combined.lower()


def test_config_nested_mounts_rejected(coi_binary, workspace_dir, tmp_path):
    """Test nested paths in config file are rejected."""
    dir1 = tmp_path / "d1"
    dir2 = tmp_path / "d2"
    dir1.mkdir()
    dir2.mkdir()

    # Create config file in workspace directory
    config_content = f"""
[[mounts.default]]
host = "{dir1}"
container = "/app"

[[mounts.default]]
host = "{dir2}"
container = "/app/subdir"
"""
    config_file = Path(workspace_dir) / ".coi.toml"
    config_file.write_text(config_content)

    # Run from workspace directory so config is loaded
    result = subprocess.run(
        [coi_binary, "run", "--", "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,  # Run from workspace directory to load .coi.toml
    )

    assert result.returncode != 0
    combined = result.stdout + result.stderr
    assert "nested" in combined.lower() or "conflict" in combined.lower()
