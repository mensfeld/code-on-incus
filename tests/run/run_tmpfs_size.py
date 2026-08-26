"""
Test that `coi run` honors [limits.disk] tmpfs_size.

`coi shell` sized the /tmp tmpfs from [limits.disk] tmpfs_size but `coi run`
ignored it, so a big build that fit under `coi shell` could ENOSPC under
`coi run`. This asserts the /tmp device carries the configured size after a
run (#726 follow-up).
"""

import subprocess
from pathlib import Path

from support.helpers import calculate_container_name


def test_run_honors_tmpfs_size(coi_binary, cleanup_containers, workspace_dir):
    """coi run must size the /tmp tmpfs device from [limits.disk] tmpfs_size."""
    slot = 6
    container_name = calculate_container_name(workspace_dir, slot)
    coi_dir = Path(workspace_dir) / ".coi"
    coi_dir.mkdir(parents=True, exist_ok=True)
    (coi_dir / "config.toml").write_text(
        '[container]\npersistent = true\n\n[limits.disk]\ntmpfs_size = "256MiB"\n'
    )

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--slot", str(slot), "--", "true"],
        capture_output=True,
        text=True,
        timeout=180,
    )
    assert result.returncode == 0, f"coi run should succeed. stderr: {result.stderr}"

    try:
        got = subprocess.run(
            ["incus", "config", "device", "get", container_name, "tmp", "size"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert got.returncode == 0, f"could not read /tmp device size: {got.stderr}"
        assert got.stdout.strip() == "256MiB", (
            f"/tmp tmpfs size should be 256MiB after coi run, got '{got.stdout.strip()}'"
        )
    finally:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
            check=False,
        )
