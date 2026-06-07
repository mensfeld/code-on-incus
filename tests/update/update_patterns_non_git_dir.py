"""
Test that coi update patterns refuses to clobber an existing non-git directory.
"""

import os
import subprocess


def test_update_patterns_rejects_non_git_directory(coi_binary, tmp_path):
    """
    Running update patterns on an existing non-git directory fails with a clear error.

    The safety guard prevents clobbering directories that exist but have no .git,
    so users cannot accidentally overwrite their own data. The check runs before
    any git command, so no network access or --dry-run flag is needed.

    Flow:
    1. Create a plain (non-git) directory
    2. Run coi update patterns --gtfobins-dir <path>
    3. Verify exit code is non-zero
    4. Verify output mentions "not a git repository"
    """
    non_git_dir = tmp_path / "not-a-repo"
    non_git_dir.mkdir()
    env = {**os.environ, "HOME": str(tmp_path)}

    result = subprocess.run(
        [coi_binary, "update", "patterns", "--gtfobins-dir", str(non_git_dir)],
        capture_output=True,
        text=True,
        timeout=10,
        env=env,
    )

    assert result.returncode != 0, "Should fail when target directory exists but is not a git repo"

    combined = result.stdout + result.stderr
    assert "not a git repository" in combined, (
        f"Error should mention 'not a git repository'. Got:\n{combined}"
    )
