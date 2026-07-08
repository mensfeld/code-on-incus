"""
End-to-end tests for git worktree support (#533): coi must make a linked-worktree
checkout usable inside the container by mounting the external git internals — the
gitdir + common dir that live OUTSIDE the workspace — WITHOUT reopening the #474
host-RCE class.

A worktree's `.git` is a file (`gitdir: <main>/.git/worktrees/<name>`), so the real
internals are outside the single workspace mount. coi resolves + mounts the common
dir read-write (so git/commits work) but re-covers its RCE sinks (hooks, config,
info/attributes, worktrees/*/config.worktree) read-only. A `.git` pointer that fails
the bidirectional-link safety guard must NOT be mounted (anti ~/.ssh-redirect).

Runs in the `git-hooks` CI group (real Incus). The workspace passed to `coi run` is
the *linked worktree checkout dir*; preserve-path is auto-forced so the container
path equals the host path and every git pointer resolves.
"""

import subprocess


def _git(*args, cwd=None, check=True):
    return subprocess.run(
        ["git", "-c", "user.email=t@t.dev", "-c", "user.name=t", *args],
        cwd=cwd,
        check=check,
        capture_output=True,
        text=True,
    )


def _setup_worktree(root):
    """Create a main repo + a linked worktree with all RCE-sink files populated.
    Returns (worktree_dir, common_dir)."""
    main = root / "main"
    main.mkdir()
    _git("init", "-q", str(main))
    _git("commit", "-q", "--allow-empty", "-m", "init", cwd=str(main))
    _git("config", "extensions.worktreeConfig", "true", cwd=str(main))

    wt = root / "wt"
    _git("worktree", "add", "-q", str(wt), cwd=str(main))

    common = main / ".git"
    # Ensure every sink exists so each read-only assertion has a real target.
    (common / "hooks").mkdir(exist_ok=True)
    (common / "info").mkdir(exist_ok=True)
    (common / "info" / "attributes").write_text("* -text\n")
    # Per-worktree config lands at common/worktrees/<name>/config.worktree.
    _git("config", "--worktree", "filter.evil.smudge", "id", cwd=str(wt))
    return wt, common


def _coi_run(coi_binary, workspace, script, timeout=180):
    return subprocess.run(
        [coi_binary, "run", "--workspace", str(workspace), "--", "sh", "-c", script],
        capture_output=True,
        text=True,
        timeout=timeout,
    )


def _assert_ro(result, host_file, original):
    combined = (result.stdout + result.stderr).lower()
    assert result.returncode != 0, f"write should have failed:\n{result.stdout}{result.stderr}"
    assert (
        "read-only" in combined
        or "read only" in combined
        or "permission denied" in combined
        or "cannot" in combined
    ), f"expected a read-only error, got:\n{result.stdout}{result.stderr}"
    assert host_file.read_text() == original, f"host file {host_file} must be unchanged"


def test_git_works_in_worktree(coi_binary, cleanup_containers, tmp_path):
    """git resolves inside a worktree checkout (external gitdir+commondir mounted)."""
    wt, _ = _setup_worktree(tmp_path)
    result = _coi_run(
        coi_binary, wt, f"git -C {wt} rev-parse --is-inside-work-tree && git -C {wt} log --oneline"
    )
    combined = result.stdout + result.stderr
    assert result.returncode == 0, f"git must work in a worktree:\n{combined}"
    assert "true" in result.stdout
    assert "init" in result.stdout, f"git log should show the commit:\n{combined}"


def test_commit_works_objects_writable(coi_binary, cleanup_containers, tmp_path):
    """A commit inside the container writes objects/refs to the (RW) common dir."""
    wt, _ = _setup_worktree(tmp_path)
    result = _coi_run(
        coi_binary,
        wt,
        f"git -C {wt} -c user.email=c@c.dev -c user.name=c commit -q --allow-empty -m in-container",
    )
    assert result.returncode == 0, (
        f"commit must succeed (objects/refs RW):\n{result.stdout}{result.stderr}"
    )
    # The new commit is visible on the host via the shared common dir.
    log = _git("-C", str(wt), "log", "--oneline").stdout
    assert "in-container" in log, f"commit did not land in the shared repo:\n{log}"


def test_external_hooks_readonly(coi_binary, cleanup_containers, tmp_path):
    """Planting a hook in the external common dir must fail (core #474 non-regression)."""
    wt, common = _setup_worktree(tmp_path)
    hook = common / "hooks" / "pre-commit"
    original = "" if not hook.exists() else hook.read_text()
    result = _coi_run(coi_binary, wt, f"echo '#!/bin/sh\\nowned' > {hook}")
    # The hook file must not have been created/modified on the host.
    assert result.returncode != 0, f"writing a hook must fail:\n{result.stdout}{result.stderr}"
    assert (not hook.exists()) or hook.read_text() == original, "host hook must be unchanged"


def test_external_config_readonly(coi_binary, cleanup_containers, tmp_path):
    """The external common-dir config must be read-only (core/hooksPath/filter RCE)."""
    wt, common = _setup_worktree(tmp_path)
    cfg = common / "config"
    original = cfg.read_text()
    result = _coi_run(coi_binary, wt, f"echo '[core]\\n\\thooksPath = /tmp/evil' >> {cfg}")
    _assert_ro(result, cfg, original)


def test_external_info_attributes_readonly(coi_binary, cleanup_containers, tmp_path):
    """The external common-dir info/attributes must be read-only."""
    wt, common = _setup_worktree(tmp_path)
    attrs = common / "info" / "attributes"
    original = attrs.read_text()
    result = _coi_run(coi_binary, wt, f"echo '* filter=evil' >> {attrs}")
    _assert_ro(result, attrs, original)


def test_external_worktree_config_readonly(coi_binary, cleanup_containers, tmp_path):
    """Per-worktree config.worktree in the external common dir must be read-only."""
    wt, common = _setup_worktree(tmp_path)
    matches = list((common / "worktrees").glob("*/config.worktree"))
    assert matches, "test setup failed: no config.worktree created"
    cw = matches[0]
    original = cw.read_text()
    result = _coi_run(coi_binary, wt, f"echo '[core]\\n\\thooksPath = /tmp/evil' >> {cw}")
    _assert_ro(result, cw, original)


def test_poisoned_gitdir_pointer_refused(coi_binary, cleanup_containers, tmp_path):
    """A `.git` file pointing at an arbitrary host dir (no worktree back-pointer /
    no object store) must NOT be mounted — the container gains no access to it."""
    secret = tmp_path / "secret"
    secret.mkdir()
    (secret / "loot").write_text("TOP-SECRET-DATA")

    poison = tmp_path / "poison"
    poison.mkdir()
    (poison / ".git").write_text(f"gitdir: {secret}\n")

    result = _coi_run(coi_binary, poison, f"cat {secret}/loot 2>&1 || echo NO_ACCESS")
    combined = result.stdout + result.stderr
    assert "TOP-SECRET-DATA" not in combined, (
        f"a poisoned gitdir pointer must not get the secret dir mounted:\n{combined}"
    )
    assert "NO_ACCESS" in result.stdout, f"the secret dir must be inaccessible:\n{combined}"


def test_normal_repo_still_protected(coi_binary, cleanup_containers, tmp_path):
    """A normal (non-worktree) repo is unaffected by the refactor: .git/hooks RO."""
    ws = tmp_path / "repo"
    ws.mkdir()
    _git("init", "-q", str(ws))
    result = _coi_run(coi_binary, ws, "echo owned > /workspace/.git/hooks/pre-commit")
    combined = (result.stdout + result.stderr).lower()
    assert result.returncode != 0, (
        f"normal-repo hook write must still fail:\n{result.stdout}{result.stderr}"
    )
    assert "read-only" in combined or "read only" in combined or "permission denied" in combined, (
        combined
    )
