"""
Regression test for issue #305: a profile that inherits its storage pool
from a parent (without declaring its own [container] section) must still
cause the health check to enumerate that inherited pool.

This is the Python mirror of TestCollectReferencedPools_WithInheritance
in internal/health/health_test.go: it proves the end-to-end path from
TOML → config.Load → ResolveProfileInheritance → health check enumeration.
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


def test_health_storage_pools_inherited_pool(coi_binary, workspace_dir):
    """A child profile with no [container] section must inherit parent's pool."""
    pool_name = "coi-test-inheritedpool"

    ok, err = _create_temp_pool(pool_name)
    if not ok:
        pytest.skip(f"Cannot create temp storage pool {pool_name}: {err}")

    try:
        profiles_dir = Path(workspace_dir) / ".coi" / "profiles"

        # Parent: declares the pool.
        parent = profiles_dir / "parent"
        parent.mkdir(parents=True)
        (parent / "config.toml").write_text(
            f'[container]\nstorage_pool = "{pool_name}"\n'
        )

        # Leaf: inherits from parent, no [container] at all.
        leaf = profiles_dir / "leaf"
        leaf.mkdir(parents=True)
        (leaf / "config.toml").write_text('inherits = "parent"\n')

        result = subprocess.run(
            [coi_binary, "health", "--format", "json", "--workspace", workspace_dir],
            capture_output=True,
            text=True,
            timeout=60,
            cwd=workspace_dir,
        )
        assert result.returncode in (0, 1), (
            f"health should exit 0 or 1. stderr: {result.stderr}"
        )

        data = json.loads(result.stdout)
        details = data["checks"]["incus_storage_pools"]["details"]
        assert pool_name in details, (
            f"Inherited pool {pool_name} must appear in pool details even "
            f"though 'leaf' never mentioned it directly. Got: {list(details.keys())}"
        )
    finally:
        _delete_temp_pool(pool_name)
