"""
Test that the storage pool health check reports the pool's driver (#659).

A pool on the `dir` driver forces `incus init` to re-unpack the whole image
on every launch — ~73% of a `coi run`'s wall time on the reporting host.
The check must make that visible end-to-end, in both output formats:

- JSON: the per-pool details carry the driver, and a `dir` pool is a warning.
- Text: the rendered message names the driver (`pool (dir): ...`) and carries
  the `dir` warning — this is the gap where the driver was once only visible
  in JSON details, so `coi health`'s text output could not answer
  "which driver is my pool on?".
"""

import json
import os
import subprocess
from pathlib import Path

import pytest


def _is_permission_error(stderr):
    """Heuristic: was `incus storage create` rejected because the caller
    does not have Incus admin access? Anything else (name collision,
    backend error, transient failure) should fail the test loud, not skip."""
    lowered = stderr.lower()
    return (
        "permission denied" in lowered
        or "not authorized" in lowered
        or "forbidden" in lowered
        or "access denied" in lowered
    )


def _create_dir_pool(name):
    result = subprocess.run(
        ["incus", "storage", "create", name, "dir"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    return result.returncode == 0, result.stderr


def _delete_pool(name):
    subprocess.run(
        ["incus", "storage", "delete", name],
        capture_output=True,
        timeout=30,
    )


def test_health_reports_dir_pool_driver(coi_binary, workspace_dir):
    """A dir pool must surface its driver and warn, in JSON and text output."""
    suffix = os.urandom(4).hex()
    pool = f"coi-test-dirpool-{suffix}"

    ok, err = _create_dir_pool(pool)
    if not ok:
        if _is_permission_error(err):
            pytest.skip(f"No permission to create storage pool {pool}: {err}")
        pytest.fail(f"Failed to create temp pool {pool}: {err}")

    try:
        profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "dirpool"
        profile_dir.mkdir(parents=True)
        (profile_dir / "config.toml").write_text(
            f'[container]\nimage = "coi-default"\nstorage_pool = "{pool}"\n'
        )

        # JSON output: driver in details, dir pool downgraded to warning.
        result = subprocess.run(
            [coi_binary, "health", "--format", "json", "--workspace", workspace_dir],
            capture_output=True,
            text=True,
            timeout=60,
            cwd=workspace_dir,
        )
        assert result.returncode in (0, 1, 2), (
            f"health should exit 0, 1, or 2. stderr: {result.stderr}"
        )

        data = json.loads(result.stdout)
        check = data["checks"]["incus_storage_pools"]

        assert pool in check["details"], (
            f"Pool {pool} should appear in pool details. Got: {list(check['details'].keys())}"
        )
        entry = check["details"][pool]
        assert entry["driver"] == "dir", f"Expected driver 'dir', got: {entry.get('driver')}"
        assert entry["status"] == "warning", (
            f"A dir pool must be a warning even with healthy usage, got: {entry.get('status')}"
        )
        assert check["status"] in ("warning", "failed"), (
            f"Overall check must be at least a warning with a dir pool, got: {check['status']}"
        )

        # Text output: the human-readable line must name the driver and warn,
        # not bury it in JSON-only details.
        result = subprocess.run(
            [coi_binary, "health", "--workspace", workspace_dir],
            capture_output=True,
            text=True,
            timeout=60,
            cwd=workspace_dir,
        )
        assert result.returncode in (0, 1, 2), (
            f"health should exit 0, 1, or 2. stderr: {result.stderr}"
        )
        assert f"{pool} (dir):" in result.stdout, (
            f"Text output should name the pool's driver as '{pool} (dir): ...'. "
            f"stdout: {result.stdout}"
        )
        assert "'dir' storage driver" in result.stdout, (
            f"Text output should carry the dir-driver warning. stdout: {result.stdout}"
        )
    finally:
        _delete_pool(pool)
