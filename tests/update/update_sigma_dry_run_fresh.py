"""
Test that `coi update sigma --dry-run` on a fresh (nonexistent) directory
prints both the sparse clone command and the sparse-checkout command without
executing them.
"""

import os
import subprocess


def test_update_sigma_dry_run_fresh_clone(coi_binary, tmp_path):
    """
    Dry-run on a nonexistent directory shows the git clone + sparse-checkout
    commands and exits 0 without creating any files.
    """
    clone_dir = tmp_path / "sigma-fresh"
    env = {**os.environ, "HOME": str(tmp_path)}

    result = subprocess.run(
        [
            coi_binary,
            "update",
            "sigma",
            "--dry-run",
            "--sigma-dir",
            str(clone_dir),
        ],
        capture_output=True,
        text=True,
        timeout=10,
        env=env,
    )

    assert result.returncode == 0, f"Dry-run on fresh dir should succeed. stderr: {result.stderr}"
    output = result.stdout
    assert "[dry-run]" in output, f"Expected '[dry-run]' marker. Got:\n{output}"
    assert "clone" in output, f"Expected 'clone' in dry-run output. Got:\n{output}"
    assert "sparse-checkout" in output, (
        f"Expected 'sparse-checkout' in dry-run output. Got:\n{output}"
    )
    assert str(clone_dir) in output, f"Expected clone dir path in output. Got:\n{output}"
    assert not clone_dir.exists(), "Dry-run must not create the clone directory"
