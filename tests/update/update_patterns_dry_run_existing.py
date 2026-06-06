"""
Test that coi update patterns --dry-run on an existing clone shows a git pull command.
"""

import subprocess


def test_update_patterns_dry_run_existing_clone(coi_binary, tmp_path):
    """
    Dry-run on a directory that already has a .git folder shows a pull command.

    Flow:
    1. Create a fake git repo (directory + .git subdirectory)
    2. Run coi update patterns --dry-run --gtfobins-dir <path>
    3. Verify exit code is 0
    4. Verify stdout contains [dry-run] and "pull" (not "clone")
    """
    clone_dir = tmp_path / "gtfobins-existing"
    clone_dir.mkdir()
    (clone_dir / ".git").mkdir()

    result = subprocess.run(
        [
            coi_binary,
            "update",
            "patterns",
            "--dry-run",
            "--gtfobins-dir",
            str(clone_dir),
        ],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, (
        f"Dry-run on existing clone should succeed. stderr: {result.stderr}"
    )

    output = result.stdout
    assert "[dry-run]" in output, f"Should show [dry-run] marker. Got:\n{output}"
    assert "pull" in output, f"Existing clone should trigger pull, not clone. Got:\n{output}"
