"""
Test storage pool validation - non-existent pool, `coi shell` (#726).

`coi shell` used to ignore [container] storage_pool entirely, only ever
launching on whichever pool the Incus `default` profile points at. It must
fail before any container work begins, just like `coi run`/`coi build`, with
the same actionable `incus storage create` example.
"""

import subprocess
from pathlib import Path


def test_shell_missing_storage_pool_fails_with_actionable_error(coi_binary, workspace_dir):
    """coi shell with a non-existent pool errors with copy-pasteable fix."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "missingpool"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        '[container]\nimage = "coi-default"\nstorage_pool = "this-pool-does-not-exist-xyz123"\n'
    )

    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--debug",
            "--profile",
            "missingpool",
            "--workspace",
            workspace_dir,
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
