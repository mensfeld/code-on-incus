"""
Integration tests for coi update patterns subcommand.

Tests the three code paths in updatePatternsCommand without requiring network
access or a real GTFOBins clone:
  - dry-run on a nonexistent directory (clone path)
  - dry-run with a custom --source URL
  - dry-run on an existing clone (pull path)
  - safety guard that refuses to clobber a non-git directory
"""

import subprocess


def test_update_patterns_dry_run_fresh_clone(coi_binary, tmp_path):
    """
    Dry-run on a nonexistent target directory shows a git clone command.

    Flow:
    1. Pick a path that does not yet exist
    2. Run coi update patterns --dry-run --gtfobins-dir <path>
    3. Verify exit code is 0
    4. Verify stdout contains [dry-run], "clone", and the target path
    """
    target_dir = str(tmp_path / "gtfobins-fresh")

    result = subprocess.run(
        [coi_binary, "update", "patterns", "--dry-run", "--gtfobins-dir", target_dir],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, (
        f"Dry-run on fresh directory should succeed. stderr: {result.stderr}"
    )

    output = result.stdout
    assert "[dry-run]" in output, f"Should show [dry-run] marker. Got:\n{output}"
    assert "clone" in output, f"Should show clone command. Got:\n{output}"
    assert target_dir in output, f"Should show target directory. Got:\n{output}"


def test_update_patterns_dry_run_custom_source(coi_binary, tmp_path):
    """
    Dry-run with a custom --source URL includes that URL in the clone command.

    Flow:
    1. Pick a nonexistent target directory
    2. Run coi update patterns --dry-run --gtfobins-dir <path> --source <url>
    3. Verify exit code is 0
    4. Verify stdout contains the custom URL
    """
    target_dir = str(tmp_path / "gtfobins-custom")
    custom_url = "https://github.com/example/custom-gtfobins.git"

    result = subprocess.run(
        [
            coi_binary,
            "update",
            "patterns",
            "--dry-run",
            "--gtfobins-dir",
            target_dir,
            "--source",
            custom_url,
        ],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, (
        f"Dry-run with custom source should succeed. stderr: {result.stderr}"
    )

    output = result.stdout
    assert custom_url in output, (
        f"Custom source URL should appear in dry-run output. Got:\n{output}"
    )


def test_update_patterns_dry_run_existing_clone(coi_binary, tmp_path):
    """
    Dry-run on a directory that already contains a .git folder shows a pull command.

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
    assert "pull" in output, (
        f"Existing clone should trigger pull, not clone. Got:\n{output}"
    )


def test_update_patterns_rejects_non_git_directory(coi_binary, tmp_path):
    """
    Running update patterns on an existing non-git directory fails with a clear error.

    The command refuses to clobber directories that exist but have no .git, so
    users cannot accidentally overwrite their own data. This path does not need
    --dry-run because the check happens before any git command is run.

    Flow:
    1. Create a non-git directory
    2. Run coi update patterns --gtfobins-dir <path> (no --dry-run)
    3. Verify exit code is non-zero
    4. Verify error output mentions "not a git repository"
    """
    non_git_dir = tmp_path / "not-a-repo"
    non_git_dir.mkdir()

    result = subprocess.run(
        [coi_binary, "update", "patterns", "--gtfobins-dir", str(non_git_dir)],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode != 0, (
        "Should fail when target directory exists but is not a git repo"
    )

    combined = result.stdout + result.stderr
    assert "not a git repository" in combined, (
        f"Error should mention 'not a git repository'. Got:\n{combined}"
    )
