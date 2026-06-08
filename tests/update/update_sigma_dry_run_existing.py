"""
Test that `coi update sigma --dry-run` on a directory that already has a .git
folder shows a pull command (not clone) and exits 0.
"""

import os
import subprocess


def test_update_sigma_dry_run_existing_clone(coi_binary, tmp_path):
    """
    Dry-run on a directory that already has a .git folder shows a pull command.
    """
    clone_dir = tmp_path / "sigma-existing"
    clone_dir.mkdir()
    (clone_dir / ".git").mkdir()
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

    assert result.returncode == 0, (
        f"Dry-run on existing clone should succeed. stderr: {result.stderr}"
    )
    output = result.stdout
    assert "[dry-run]" in output, f"Expected '[dry-run]' marker. Got:\n{output}"
    assert "pull" in output, f"Existing clone should trigger pull, not clone. Got:\n{output}"
    assert "clone" not in output, f"Existing clone should not re-clone. Got:\n{output}"
