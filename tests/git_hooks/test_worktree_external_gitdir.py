"""
End-to-end tests for git worktree support (#533): coi must make a linked-worktree
checkout usable inside the container by mounting the external git internals — the
gitdir + common dir that live OUTSIDE the workspace — WITHOUT reopening the #474
host-RCE class.

A worktree's `.git` is a file (`gitdir: <main>/.git/worktrees/<name>`), so the real
internals are outside the single workspace mount. coi resolves + mounts the common
dir read-write (so git/commits work) but re-covers its RCE sinks (hooks, config,
info/attributes, worktrees/*/config.worktree) read-only — including an empty
read-only overlay when a sink is ABSENT, so a contained agent cannot plant one. A
`.git` pointer that fails the bidirectional-link safety guard must NOT be mounted
(anti ~/.ssh-redirect).

Runs in the `git-hooks` CI group (real Incus). The workspace passed to `coi run` is
the *linked worktree checkout dir*; preserve-path is auto-forced so the container
path equals the host path and every git pointer resolves.
"""

import shutil
import subprocess

import pytest

from support.helpers import calculate_container_name


def _git(*args, cwd=None, check=True):
    return subprocess.run(
        ["git", "-c", "user.email=t@t.dev", "-c", "user.name=t", *args],
        cwd=cwd,
        check=check,
        capture_output=True,
        text=True,
    )


@pytest.fixture
def wt_cleanup(coi_binary):
    """Delete containers for the actual workspace paths each test launches. The
    shared cleanup_containers fixture keys on the workspace_dir fixture, which these
    tests do not use as the workspace, so it would delete nothing. coi-run containers
    are ephemeral (self-delete), but an interrupted run can linger — clean explicitly."""
    workspaces = []
    yield workspaces
    for ws in workspaces:
        for slot in range(1, 11):
            subprocess.run(
                [
                    coi_binary,
                    "container",
                    "delete",
                    calculate_container_name(str(ws), slot),
                    "--force",
                ],
                capture_output=True,
                text=True,
            )


def _setup_worktree(root, track, with_worktree_config=True):
    """Create a main repo + a linked worktree with the RCE-sink files populated.
    Returns (worktree_dir, common_dir)."""
    main = root / "main"
    main.mkdir()
    _git("init", "-q", str(main))
    _git("commit", "-q", "--allow-empty", "-m", "init", cwd=str(main))
    _git("config", "extensions.worktreeConfig", "true", cwd=str(main))

    wt = root / "wt"
    _git("worktree", "add", "-q", str(wt), cwd=str(main))
    track.append(wt)

    common = main / ".git"
    (common / "hooks").mkdir(exist_ok=True)
    (common / "info").mkdir(exist_ok=True)
    (common / "info" / "attributes").write_text("* -text\n")
    if with_worktree_config:
        _git("config", "--worktree", "filter.evil.smudge", "id", cwd=str(wt))
    return wt, common


def _coi_run(coi_binary, workspace, script, timeout=180):
    return subprocess.run(
        [coi_binary, "run", "--workspace", str(workspace), "--", "sh", "-c", script],
        capture_output=True,
        text=True,
        timeout=timeout,
    )


def _assert_write_blocked(result):
    combined = (result.stdout + result.stderr).lower()
    assert result.returncode != 0, f"write should have failed:\n{result.stdout}{result.stderr}"
    assert "read-only" in combined or "read only" in combined or "permission denied" in combined, (
        f"expected a read-only error, got:\n{result.stdout}{result.stderr}"
    )


def _assert_file_readonly(coi_binary, wt, host_file, container_path):
    """Prove the sink is mounted+readable, THEN prove it's not writable — so a
    regression that stops mounting the common dir entirely can't false-green as
    'read-only' (a missing path would fail the read precondition)."""
    read = _coi_run(coi_binary, wt, f"cat {container_path}")
    assert read.returncode == 0, (
        f"sink must be mounted+readable inside container:\n{read.stdout}{read.stderr}"
    )
    original = host_file.read_text()
    _assert_write_blocked(_coi_run(coi_binary, wt, f"echo EVIL >> {container_path}"))
    assert host_file.read_text() == original, f"host file {host_file} must be unchanged"


def test_git_works_in_worktree(coi_binary, cleanup_containers, wt_cleanup, tmp_path):
    """git resolves inside a worktree checkout (external gitdir+commondir mounted)."""
    wt, _ = _setup_worktree(tmp_path, wt_cleanup)
    result = _coi_run(
        coi_binary, wt, f"git -C {wt} rev-parse --is-inside-work-tree && git -C {wt} log --oneline"
    )
    combined = result.stdout + result.stderr
    assert result.returncode == 0, f"git must work in a worktree:\n{combined}"
    assert "true" in result.stdout
    assert "init" in result.stdout, f"git log should show the commit:\n{combined}"


def test_commit_works_objects_writable(coi_binary, cleanup_containers, wt_cleanup, tmp_path):
    """A commit inside the container writes objects/refs to the (RW) common dir."""
    wt, _ = _setup_worktree(tmp_path, wt_cleanup)
    result = _coi_run(
        coi_binary,
        wt,
        f"git -C {wt} -c user.email=c@c.dev -c user.name=c commit -q --allow-empty -m in-container",
    )
    assert result.returncode == 0, (
        f"commit must succeed (objects/refs RW):\n{result.stdout}{result.stderr}"
    )
    # The new commit is visible on the host via the shared common dir (proves RW).
    log = _git("-C", str(wt), "log", "--oneline").stdout
    assert "in-container" in log, f"commit did not land in the shared repo:\n{log}"


def test_external_hooks_readonly(coi_binary, cleanup_containers, wt_cleanup, tmp_path):
    """Planting a hook in the (existing) external hooks dir must fail (core #474)."""
    wt, common = _setup_worktree(tmp_path, wt_cleanup)
    hooks = common / "hooks"
    # readable/mounted precondition: hooks dir is listable inside the container.
    ls = _coi_run(coi_binary, wt, f"ls {hooks}")
    assert ls.returncode == 0, f"hooks dir must be mounted:\n{ls.stdout}{ls.stderr}"
    _assert_write_blocked(_coi_run(coi_binary, wt, f"echo owned > {hooks}/pre-commit"))
    assert not (hooks / "pre-commit").exists(), "host hook must not have been created"


def test_external_config_readonly(coi_binary, cleanup_containers, wt_cleanup, tmp_path):
    """The external common-dir config must be read-only (core/hooksPath/filter RCE)."""
    wt, common = _setup_worktree(tmp_path, wt_cleanup)
    _assert_file_readonly(coi_binary, wt, common / "config", f"{common}/config")


def test_external_info_attributes_readonly(coi_binary, cleanup_containers, wt_cleanup, tmp_path):
    """The external common-dir info/attributes must be read-only."""
    wt, common = _setup_worktree(tmp_path, wt_cleanup)
    _assert_file_readonly(
        coi_binary, wt, common / "info" / "attributes", f"{common}/info/attributes"
    )


def test_external_worktree_config_readonly(coi_binary, cleanup_containers, wt_cleanup, tmp_path):
    """Per-worktree config.worktree in the external common dir must be read-only."""
    wt, common = _setup_worktree(tmp_path, wt_cleanup)
    matches = list((common / "worktrees").glob("*/config.worktree"))
    assert matches, "test setup failed: no config.worktree created"
    cw = matches[0]
    _assert_file_readonly(coi_binary, wt, cw, str(cw))


def test_absent_hooks_dir_not_plantable(coi_binary, cleanup_containers, wt_cleanup, tmp_path):
    """#533/#474: if the common dir has NO hooks/, an agent still cannot create it
    (an empty read-only placeholder is overlaid). The common dir is mounted RW, so
    without the overlay this would be a host-RCE plant vector."""
    wt, common = _setup_worktree(tmp_path, wt_cleanup)
    shutil.rmtree(common / "hooks")  # empty-template / deleted-hooks repo
    result = _coi_run(
        coi_binary, wt, f"mkdir -p {common}/hooks 2>&1; echo owned > {common}/hooks/pre-commit"
    )
    assert result.returncode != 0, f"planting hooks/ must fail:\n{result.stdout}{result.stderr}"
    assert not (common / "hooks" / "pre-commit").exists(), "host must not gain a planted hook"


def test_absent_worktree_config_not_plantable(coi_binary, cleanup_containers, wt_cleanup, tmp_path):
    """#533/#474: a worktree WITHOUT a config.worktree still can't have one planted
    (empty read-only overlay), even though its worktrees/<name>/ dir is RW."""
    wt, common = _setup_worktree(tmp_path, wt_cleanup, with_worktree_config=False)
    name = next((common / "worktrees").iterdir()).name
    target = common / "worktrees" / name / "config.worktree"
    assert not target.exists(), "precondition: config.worktree absent"
    result = _coi_run(coi_binary, wt, f"echo '[core]\\n\\thooksPath = /tmp/evil' > {target}")
    assert result.returncode != 0, (
        f"planting config.worktree must fail:\n{result.stdout}{result.stderr}"
    )
    assert not target.exists(), "host must not gain a planted config.worktree"


def test_poisoned_gitdir_pointer_refused(coi_binary, cleanup_containers, wt_cleanup, tmp_path):
    """A `.git` file pointing at an arbitrary host dir (no worktree back-pointer /
    no object store) must NOT be mounted — the container gains no access to it. If
    the guard were bypassed, the pointed-at dir would be mounted preserve-path and
    `cat` would succeed, failing this test."""
    secret = tmp_path / "secret"
    secret.mkdir()
    (secret / "loot").write_text("TOP-SECRET-DATA")

    poison = tmp_path / "poison"
    poison.mkdir()
    (poison / ".git").write_text(f"gitdir: {secret}\n")
    wt_cleanup.append(poison)

    result = _coi_run(coi_binary, poison, f"echo RAN; cat {secret}/loot 2>&1 || echo NO_ACCESS")
    combined = result.stdout + result.stderr
    assert "RAN" in result.stdout, f"coi run should still execute the command:\n{combined}"
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
