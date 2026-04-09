"""
Test storage pool validation - existing pool.

When the configured pool actually exists, validation passes and the
container launches normally.
"""

import subprocess
from pathlib import Path


def _default_pool_name():
    """Return the name of the Incus default profile's storage pool, or None."""
    try:
        out = subprocess.check_output(
            ["incus", "profile", "show", "default"],
            text=True,
            timeout=10,
        )
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired, FileNotFoundError):
        return None
    for line in out.splitlines():
        line = line.strip()
        if line.startswith("pool:"):
            return line.split(":", 1)[1].strip()
    return None


def test_existing_storage_pool_validation_passes(coi_binary, cleanup_containers, workspace_dir):
    """A profile referencing the real default pool should run successfully."""
    pool = _default_pool_name()
    if not pool:
        import pytest

        pytest.skip("Cannot determine default Incus storage pool")

    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "validpool"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        f'[container]\nimage = "coi-default"\nstorage_pool = "{pool}"\n'
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--profile",
            "validpool",
            "--workspace",
            workspace_dir,
            "--",
            "echo",
            "valid-pool-ok",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    combined = result.stdout + result.stderr
    assert result.returncode == 0, f"Run should succeed with valid pool. Got:\n{combined}"
    assert "valid-pool-ok" in combined, (
        f"Container should produce expected output. Got:\n{combined}"
    )
