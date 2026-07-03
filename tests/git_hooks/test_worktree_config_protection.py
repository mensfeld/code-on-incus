"""
Protect the git config/attribute sinks that can drive host code execution.

git runs filter/diff/textconv driver commands (and core.hooksPath) defined in
git *config* files, named via *attributes* files, during routine operations
(checkout/status/diff). COI already mounts .git/config and .git/hooks read-only;
these tests cover the remaining sinks:

- .git/info/attributes  — names drivers
- .git/config.worktree  — per-repo worktree config (read when extensions.worktreeConfig=true)
- .git/worktrees/<name>/config.worktree — per-worktree config (same)

If a container could write any of these, it could plant/define a driver that
runs on the host at the next git operation. They must be read-only.
"""

import subprocess
from pathlib import Path

import pytest


def _write_blocked(coi_binary, workspace_dir, container_path):
    """Attempt to append to container_path from inside the container; return CompletedProcess."""
    return subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            f"echo '[core]' >> {container_path}",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )


def _assert_blocked(result, host_file, original):
    combined = (result.stdout + result.stderr).lower()
    assert result.returncode != 0, f"write should have failed; got rc=0. output: {combined}"
    assert "read-only" in combined or "read only" in combined or "permission denied" in combined, (
        f"expected read-only/permission error, got: {combined}"
    )
    assert host_file.read_text() == original, "host file must be unchanged"


def test_git_info_attributes_readonly(coi_binary, workspace_dir, cleanup_containers):
    """.git/info/attributes must be read-only (it names filter/diff/textconv drivers)."""
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)
    attrs = Path(workspace_dir) / ".git" / "info" / "attributes"
    original = "* text=auto\n"
    attrs.write_text(original)

    result = _write_blocked(coi_binary, workspace_dir, "/workspace/.git/info/attributes")
    _assert_blocked(result, attrs, original)


def test_git_config_worktree_readonly(coi_binary, workspace_dir, cleanup_containers):
    """.git/config.worktree must be read-only (a config sink under worktreeConfig)."""
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)
    cw = Path(workspace_dir) / ".git" / "config.worktree"
    original = "# worktree config\n"
    cw.write_text(original)

    result = _write_blocked(coi_binary, workspace_dir, "/workspace/.git/config.worktree")
    _assert_blocked(result, cw, original)


@pytest.mark.xfail(
    reason="Known gap: per-worktree config at the dynamic path .git/worktrees/<name>/"
    "config.worktree is not covered by default protected_paths (they are literal, no "
    "glob). This test passed spuriously before the #530 UID-mapping fix — every "
    "workspace write failed for uid reasons, masking the missing protection. Fixing it "
    "needs glob-expanded protected paths (a security-mount change) and is tracked "
    "separately.",
    strict=False,
)
def test_per_worktree_config_readonly(coi_binary, workspace_dir, cleanup_containers):
    """.git/worktrees/<name>/config.worktree must be read-only (the reproduced M2 sink)."""
    git = ["git", "-C", workspace_dir, "-c", "user.email=t@t", "-c", "user.name=t"]
    try:
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)
        subprocess.run(
            [*git, "commit", "--allow-empty", "-m", "init"], check=True, capture_output=True
        )
        subprocess.run(
            [*git, "config", "extensions.worktreeConfig", "true"], check=True, capture_output=True
        )
        wt_path = str(Path(workspace_dir) / "wt")
        subprocess.run([*git, "worktree", "add", wt_path], check=True, capture_output=True)
        # Create the per-worktree config file (requires worktreeConfig).
        subprocess.run(
            ["git", "-C", wt_path, "config", "--worktree", "filter.evil.smudge", "id"],
            check=True,
            capture_output=True,
        )
    except subprocess.CalledProcessError as e:
        pytest.skip(f"git worktree setup unavailable: {e.stderr.decode(errors='replace')}")

    cw = Path(workspace_dir) / ".git" / "worktrees" / "wt" / "config.worktree"
    if not cw.exists():
        pytest.skip("config.worktree was not created by git worktree config")
    original = cw.read_text()

    result = _write_blocked(
        coi_binary, workspace_dir, "/workspace/.git/worktrees/wt/config.worktree"
    )
    _assert_blocked(result, cw, original)
