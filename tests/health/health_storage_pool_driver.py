"""
Test that the storage pool health check reports the pool's driver (#659).

A pool on the `dir` driver forces `incus init` to re-unpack the whole image
on every launch — ~73% of a `coi run`'s wall time on the reporting host.
The check must make that visible end-to-end: the per-pool details carry the
driver, a `dir` pool is (at least) a warning, and the check message — printed
verbatim by the text renderer — names the driver (`pool (dir): ...`) and
carries the `dir` warning, so `coi health`'s human-readable output answers
"which driver is my pool on?".

Robustness: on CI runners the freshly created pool can legitimately report a
"failed" per-pool entry (usage query unparseable) or a failed usage threshold
(a dir pool mirrors the root filesystem, which may be nearly full). The
driver and warning assertions hold in all of those states; only the exact
"warning" status is asserted when usage is actually healthy.
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


def test_health_reports_dir_pool_driver(coi_binary, workspace_dir):
    """A dir pool must surface its driver and the dir warning end-to-end."""
    pool = f"coi-test-dirpool-{os.urandom(4).hex()}"

    ok, err = create_storage_pool(pool, driver="dir")
    if not ok:
        if is_incus_permission_error(err):
            pytest.skip(f"No permission to create storage pool {pool}: {err}")
        pytest.fail(f"Failed to create temp pool {pool}: {err}")

    try:
        profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "dirpool"
        profile_dir.mkdir(parents=True)
        (profile_dir / "config.toml").write_text(
            f'[container]\nimage = "coi-default"\nstorage_pool = "{pool}"\n'
        )

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

        # The driver is known independently of the usage query, so it must be
        # present even when the entry is on the error path.
        assert entry.get("driver") == "dir", f"Expected driver 'dir', got: {entry.get('driver')}"

        # The dir warning rides the check message in every state, and the
        # text renderer prints the message verbatim — so asserting on the
        # message covers the human-readable output too.
        assert "'dir' storage driver" in check["message"], (
            f"Check message should carry the dir-driver warning. Message: {check['message']}"
        )
        assert f"{pool} (dir):" in check["message"], (
            f"Check message should name the pool's driver as '{pool} (dir): ...'. "
            f"Message: {check['message']}"
        )

        assert check["status"] in ("warning", "failed"), (
            f"Overall check must be at least a warning with a dir pool, got: {check['status']}"
        )

        if "error" in entry:
            # Usage query failed on this runner — driver/warning assertions
            # above already covered the feature; the status is "failed" for
            # an environmental reason, not a product bug.
            assert entry["status"] == "failed"
        elif entry["free_gib"] < 5 or entry["used_pct"] > 80:
            # Usage alone already puts the pool at warning/failed on this
            # runner (a dir pool mirrors the root filesystem), so the exact
            # status is environment-dependent.
            assert entry["status"] in ("warning", "failed")
        else:
            # Healthy usage: the warning can only come from the dir driver.
            assert entry["status"] == "warning", (
                f"A dir pool with healthy usage must be a warning, got: {entry['status']}"
            )
    finally:
        delete_storage_pool(pool)
