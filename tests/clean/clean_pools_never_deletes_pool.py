"""
Test that `coi clean --pools` never deletes the storage pool itself,
only the COI containers within it.
"""

import subprocess

import pytest


def _create_temp_pool(name):
    result = subprocess.run(
        ["incus", "storage", "create", name, "dir"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    return result.returncode == 0


def _delete_temp_pool(name):
    subprocess.run(
        ["incus", "storage", "delete", name],
        capture_output=True,
        timeout=30,
    )


def _pool_exists(name):
    result = subprocess.run(
        ["incus", "storage", "list", "--format=csv"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        return False
    return any(line.startswith(name + ",") for line in result.stdout.splitlines())


def test_clean_pools_never_deletes_pool(coi_binary, workspace_dir):
    """After `coi clean --pools --force`, the pool itself remains."""
    pool_name = "coi-test-keeppool"

    if not _create_temp_pool(pool_name):
        pytest.skip("Cannot create temp storage pool")

    try:
        # Run clean --pools --force on an empty unreferenced pool.
        result = subprocess.run(
            [
                coi_binary,
                "clean",
                "--pools",
                "--force",
                "--workspace",
                workspace_dir,
            ],
            capture_output=True,
            text=True,
            timeout=60,
            cwd=workspace_dir,
        )
        assert result.returncode == 0, f"clean should succeed. stderr: {result.stderr}"

        # Pool itself must still exist
        assert _pool_exists(pool_name), f"Pool {pool_name} should still exist after clean --pools."
    finally:
        _delete_temp_pool(pool_name)
