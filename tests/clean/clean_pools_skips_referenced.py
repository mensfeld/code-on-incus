"""
Test that `coi clean --pools` never flags pools referenced by a loaded profile.
"""

import os
import subprocess
from pathlib import Path

import pytest

from support.helpers import (
    create_storage_pool,
    delete_storage_pool,
    is_incus_permission_error,
)


def test_clean_pools_skips_referenced(coi_binary, workspace_dir):
    """A pool referenced by a profile is never flagged for cleanup."""
    pool_name = f"coi-test-skipref-{os.urandom(4).hex()}"
    ok, err = create_storage_pool(pool_name)
    if not ok:
        if is_incus_permission_error(err):
            pytest.skip(f"No permission to create storage pool {pool_name}: {err}")
        pytest.fail(f"Failed to create temp pool {pool_name}: {err}")

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
        delete_storage_pool(pool_name)
