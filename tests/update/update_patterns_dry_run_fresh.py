"""
Test that coi update patterns --dry-run on a nonexistent directory shows a git clone command.
"""

import os
import subprocess


def test_update_patterns_dry_run_fresh_clone(coi_binary, tmp_path):
    """
    Dry-run on a nonexistent target directory prints a git clone command.

    Flow:
    1. Pick a path that does not yet exist inside tmp_path
    2. Run coi update patterns --dry-run --gtfobins-dir <path>
    3. Verify exit code is 0
    4. Verify stdout contains [dry-run], "clone", and the target path
    """
    target_dir = str(tmp_path / "gtfobins-fresh")
    env = {**os.environ, "HOME": str(tmp_path)}

    result = subprocess.run(
        [coi_binary, "update", "patterns", "--dry-run", "--gtfobins-dir", target_dir],
        capture_output=True,
        text=True,
        timeout=10,
        env=env,
    )

    assert result.returncode == 0, (
        f"Dry-run on fresh directory should succeed. stderr: {result.stderr}"
    )

    output = result.stdout
    assert "[dry-run]" in output, f"Should show [dry-run] marker. Got:\n{output}"
    assert "clone" in output, f"Should show clone command. Got:\n{output}"
    assert target_dir in output, f"Should show target directory. Got:\n{output}"
