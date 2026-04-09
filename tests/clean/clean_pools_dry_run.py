"""
Test that `coi clean --pools --dry-run` lists targets but deletes nothing.
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


def _delete_container(name):
    subprocess.run(
        ["incus", "delete", name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_clean_pools_dry_run(coi_binary, workspace_dir):
    """--dry-run lists what would be deleted but deletes nothing."""
    pool_name = "coi-test-dryrunpool"
    container_name = "coi-test-dryrun-container"

    if not _create_temp_pool(pool_name):
        pytest.skip("Cannot create temp storage pool")

    try:
        result = subprocess.run(
            [
                "incus",
                "init",
                "images:ubuntu/22.04",
                container_name,
                "-s",
                pool_name,
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )
        if result.returncode != 0:
            pytest.skip(f"Cannot init container in temp pool: {result.stderr}")

        try:
            result = subprocess.run(
                [
                    coi_binary,
                    "clean",
                    "--pools",
                    "--dry-run",
                    "--workspace",
                    workspace_dir,
                ],
                capture_output=True,
                text=True,
                timeout=60,
                cwd=workspace_dir,
            )
            assert result.returncode == 0, f"dry-run should succeed. stderr: {result.stderr}"

            combined = result.stdout + result.stderr
            assert container_name in combined, (
                f"Dry-run should list the container. Got:\n{combined}"
            )

            # Container should still exist
            check = subprocess.run(
                ["incus", "list", container_name, "--format=csv"],
                capture_output=True,
                text=True,
                timeout=10,
            )
            assert container_name in check.stdout, (
                f"Container should still exist after dry-run. Got:\n{check.stdout}"
            )
        finally:
            _delete_container(container_name)
    finally:
        _delete_temp_pool(pool_name)
