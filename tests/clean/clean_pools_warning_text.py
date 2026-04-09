"""
Test that `coi clean --pools` prints the cross-project warning before
listing pools to delete.
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


def test_clean_pools_warning_text(coi_binary, workspace_dir):
    """The cross-project visibility warning text appears in clean output."""
    pool_name = "coi-test-warnpool"
    container_name = "coi-test-warn-container"

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
            # --dry-run shows the warning without prompting
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

            combined = result.stdout + result.stderr
            assert "WARNING" in combined, f"Output should include WARNING text. Got:\n{combined}"
            assert "other" in combined.lower() and "projects" in combined.lower(), (
                f"Warning should mention other projects. Got:\n{combined}"
            )
        finally:
            _delete_container(container_name)
    finally:
        _delete_temp_pool(pool_name)
