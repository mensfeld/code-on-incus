"""
Test that the storage pool health check enumerates every profile's pool.

This is the regression test for issue #305: the check must inspect every
pool referenced by any profile, not just the default one. The scenario
creates two temp pools and three profiles (two explicit + one inheriting)
and asserts that both pools appear in the health JSON details.
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


def _create_temp_pool(name):
    """Create a temp pool. Returns (ok, stderr). The caller is expected to
    treat ok=False as a hard failure unless stderr matches a permission
    error, in which case the whole test should pytest.skip."""
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


def test_health_storage_pools_multi_profile(coi_binary, workspace_dir):
    """Health check must report every pool referenced by any profile."""
    # Unique pool names per test run avoid name-collision flakes from
    # leftover pools of a previous interrupted run.
    suffix = os.urandom(4).hex()
    pool_a = f"coi-test-poola-{suffix}"
    pool_b = f"coi-test-poolb-{suffix}"

    ok_a, err_a = _create_temp_pool(pool_a)
    if not ok_a:
        if _is_permission_error(err_a):
            pytest.skip(f"No permission to create storage pool {pool_a}: {err_a}")
        pytest.fail(f"Failed to create temp pool {pool_a}: {err_a}")

    ok_b, err_b = _create_temp_pool(pool_b)
    if not ok_b:
        _delete_temp_pool(pool_a)
        if _is_permission_error(err_b):
            pytest.skip(f"No permission to create storage pool {pool_b}: {err_b}")
        pytest.fail(f"Failed to create temp pool {pool_b}: {err_b}")

    try:
        profiles_dir = Path(workspace_dir) / ".coi" / "profiles"

        # Profile A: explicit pool A
        explicit_a = profiles_dir / "explicit_a"
        explicit_a.mkdir(parents=True)
        (explicit_a / "config.toml").write_text(
            f'[container]\nimage = "coi-default"\nstorage_pool = "{pool_a}"\n'
        )

        # Profile B: explicit pool B
        explicit_b = profiles_dir / "explicit_b"
        explicit_b.mkdir(parents=True)
        (explicit_b / "config.toml").write_text(
            f'[container]\nimage = "coi-default"\nstorage_pool = "{pool_b}"\n'
        )

        # Child: inherits explicit_a, no [container] of its own — must still
        # resolve to pool A via profile-to-profile inheritance at load time.
        child = profiles_dir / "child"
        child.mkdir(parents=True)
        (child / "config.toml").write_text('inherits = "explicit_a"\n')

        result = subprocess.run(
            [coi_binary, "health", "--format", "json", "--workspace", workspace_dir],
            capture_output=True,
            text=True,
            timeout=60,
            cwd=workspace_dir,
        )
        assert result.returncode in (0, 1), f"health should exit 0 or 1. stderr: {result.stderr}"

        data = json.loads(result.stdout)
        checks = data["checks"]

        # Lock in the 0.8.0 plural rename.
        assert "incus_storage_pools" in checks, (
            f"Expected plural 'incus_storage_pools' check name. Got: {list(checks.keys())}"
        )

        details = checks["incus_storage_pools"]["details"]
        assert pool_a in details, (
            f"Pool {pool_a} (explicit + inherited) should appear in pool details. "
            f"Got: {list(details.keys())}"
        )
        assert pool_b in details, (
            f"Pool {pool_b} (explicit) should appear in pool details. Got: {list(details.keys())}"
        )
    finally:
        _delete_temp_pool(pool_a)
        _delete_temp_pool(pool_b)
