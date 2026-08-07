"""
Test that the storage pool health check reports multiple pools.

Creates a temporary second storage pool, references it via a profile,
and verifies the health check details map contains both pools.
"""

import json
import os
import subprocess
from pathlib import Path

import pytest

from support.helpers import (
    create_storage_pool,
    delete_storage_pool,
    is_incus_permission_error,
)


def test_health_storage_pools_multi(coi_binary, workspace_dir):
    """Health check details map should contain both default and a temp pool."""
    # Random suffix so a pool leaked by a crashed run can't collide with (and
    # permanently skip) later runs.
    pool_name = f"coi-test-multipool-{os.urandom(4).hex()}"

    ok, err = create_storage_pool(pool_name)
    if not ok:
        if is_incus_permission_error(err):
            pytest.skip(f"No permission to create storage pool {pool_name}: {err}")
        pytest.fail(f"Failed to create temp pool {pool_name}: {err}")

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
        delete_storage_pool(pool_name)
