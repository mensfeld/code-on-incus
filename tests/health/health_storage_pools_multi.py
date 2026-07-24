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
        # A specific-check test must not gate on the AGGREGATE health exit code:
        # exit 2 means *some* one of the ~34 checks failed, which is unrelated to
        # whether the storage-pools check works and flakes on loaded CI runners.
        # Accept 0/1/2 and assert the incus_storage_pools check specifically below.
        assert result.returncode in (0, 1, 2), (
            f"health exited {result.returncode}.\n"
            f"--- report ---\n{result.stdout}\n--- stderr ---\n{result.stderr}"
        )

        data = json.loads(result.stdout)
        assert "incus_storage_pools" in data["checks"], (
            "incus_storage_pools check should be present in health output"
        )
        pool_check = data["checks"]["incus_storage_pools"]
        # The storage-pools check itself must be healthy — this test is about it;
        # an unrelated check failing is what we deliberately tolerate above.
        assert pool_check["status"] in ("ok", "warning"), (
            f"incus_storage_pools status should be ok or warning, got: {pool_check['status']}.\n"
            f"--- report ---\n{result.stdout}"
        )
        details = pool_check["details"]
        assert pool_name in details, (
            f"Temp pool {pool_name} should appear in pool details. Got: {list(details.keys())}"
        )
    finally:
        _delete_temp_pool(pool_name)
