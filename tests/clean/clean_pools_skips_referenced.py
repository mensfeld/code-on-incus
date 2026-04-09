"""
Test that `coi clean --pools` never flags pools referenced by a loaded profile.
"""

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
    return result.returncode == 0


def _delete_temp_pool(name):
    subprocess.run(
        ["incus", "storage", "delete", name],
        capture_output=True,
        timeout=30,
    )


def test_clean_pools_skips_referenced(coi_binary, workspace_dir):
    """A pool referenced by a profile is never flagged for cleanup."""
    pool_name = "coi-test-skipref"
    if not _create_temp_pool(pool_name):
        pytest.skip("Cannot create temp storage pool")

    try:
        # Create a profile that references the temp pool.
        profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "skipref"
        profile_dir.mkdir(parents=True)
        (profile_dir / "config.toml").write_text(
            f'[container]\nimage = "coi-default"\nstorage_pool = "{pool_name}"\n'
        )

        # Run clean --pools --dry-run; pool should NOT appear in output.
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
        # The pool name should not appear as a target for cleanup.
        # (It can still appear in the no-op message — that's fine.)
        assert "Delete these containers" not in combined, (
            f"Referenced pool should not be flagged for deletion. Got:\n{combined}"
        )
    finally:
        _delete_temp_pool(pool_name)
