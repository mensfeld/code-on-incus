"""
Test storage pool validation - non-existent pool.

A profile pointing at a pool that doesn't exist must fail before any
container work begins, with an actionable `incus storage create` example.
"""

import subprocess
from pathlib import Path


def test_missing_storage_pool_fails_with_actionable_error(coi_binary, workspace_dir):
    """coi run with a non-existent pool errors with copy-pasteable fix."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "missingpool"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        '[container]\nimage = "coi-default"\nstorage_pool = "this-pool-does-not-exist-xyz123"\n'
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--profile",
            "missingpool",
            "--workspace",
            workspace_dir,
            "--",
            "echo",
            "should-not-run",
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, f"Should fail with non-existent pool. stdout: {result.stdout}"
    combined = result.stdout + result.stderr
    assert "this-pool-does-not-exist-xyz123" in combined, (
        f"Error should reference the missing pool name. Got:\n{combined}"
    )
    assert "incus storage create" in combined, (
        f"Error should include 'incus storage create' example. Got:\n{combined}"
    )
    # Container should never have started
    assert "should-not-run" not in result.stdout, (
        "Container should not have run. Pool validation must fire upstream of launch."
    )
