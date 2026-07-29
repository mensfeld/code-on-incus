"""
Test that the storage pool health check reports multiple pools.

Creates a temporary second storage pool, references it via a profile,
and verifies the health check details map contains both pools.
"""

import json
import subprocess
from pathlib import Path

import pytest


def _create_temp_pool(name):
    result = subprocess.run(
        ["incus", "storage", "create", name, "dir"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    return result.returncode == 0, result.stderr


def _delete_temp_pool(name):
    subprocess.run(
        ["incus", "storage", "delete", name],
        capture_output=True,
        timeout=30,
    )


def test_health_storage_pools_multi(coi_binary, workspace_dir):
    """Health check details map should contain both default and a temp pool."""
    pool_name = "coi-test-multipool"

    # Skip if we cannot create a temp pool (e.g. no admin permission).
    ok, err = _create_temp_pool(pool_name)
    if not ok:
        pytest.skip(f"Cannot create temp storage pool: {err}")

    try:
        # Reference the temp pool from a profile so it shows up in the check.
        profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "multipool"
        profile_dir.mkdir(parents=True)
        (profile_dir / "config.toml").write_text(
            f'[container]\nimage = "coi-default"\nstorage_pool = "{pool_name}"\n'
        )

        result = subprocess.run(
            [coi_binary, "health", "--format", "json", "--workspace", workspace_dir],
            capture_output=True,
            text=True,
            timeout=60,
            cwd=workspace_dir,
        )
        # Don't gate on the AGGREGATE health exit code. Exit 2 means some check
        # failed — and here it is routinely the incus_storage_pools check itself:
        # a pool that can't be fully evaluated on a CI runner reports a per-pool
        # status of "failed" (the sibling health_storage_pool.py tolerates exactly
        # this). That does not stop the pool from being ENUMERATED, which is all
        # this test verifies. Accept 0/1/2 and assert enumeration, not health.
        assert result.returncode in (0, 1, 2), (
            f"health exited {result.returncode}.\n"
            f"--- report ---\n{result.stdout}\n--- stderr ---\n{result.stderr}"
        )

        data = json.loads(result.stdout)
        assert "incus_storage_pools" in data["checks"], (
            "incus_storage_pools check should be present in health output"
        )
        details = data["checks"]["incus_storage_pools"].get("details", {})
        # The temp pool must be ENUMERATED in the per-pool details map, regardless
        # of whether its (or another pool's) per-pool status is "failed" in CI.
        assert pool_name in details, (
            f"Temp pool {pool_name} should appear in pool details. Got: {list(details.keys())}"
        )
    finally:
        _delete_temp_pool(pool_name)
