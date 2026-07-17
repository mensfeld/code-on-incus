"""Reproduction tests for issue #610 — stale ``protect-*`` disk devices wedge a
persistent container on restart.

Background
----------
COI protects security-sensitive paths by attaching one read-only Incus *disk*
device per path (``protect-husky``, ``protect-claude-settingsjson``, ...) whose
``source=`` points at ``<workspace>/<path>`` on the host. Incus validates every
disk device's source at container **start**: if the source is missing it refuses
to start the container with::

    Failed start validation for device "protect-husky":
    Missing source path "<workspace>/.husky" for disk "protect-husky"

On a FRESH launch this is fine — ``SetupSecurityMounts`` materializes the default
paths (creates ``.husky``/``.vscode`` dirs and ``.claude/settings*.json``
placeholders) before attaching the device, so the source always exists.

The bug was on **reuse/restart of a persistent container**. The reuse branches in
``internal/session/setup.go`` originally reconciled only *port* devices
(``RemoveStalePortDevices``) and never re-ran ``SetupSecurityMounts`` nor
reconciled the ``protect-*`` devices. So if a protected path was removed from the
workspace while the container was stopped, the persistent container kept a
``protect-*`` device pointing at a now-missing source, the next start failed, and
a fresh ``coi run`` never self-healed it.

The fix adds ``ReconcileProtectedDevices`` to both reuse branches (before
``mgr.Start()``): each ``protect-*`` device whose host source has gone is either
re-materialized (COI's own default paths, so protection is preserved) or removed
(user-added paths, where "gone" means "nothing to protect").

These tests drive the real ``coi run`` persistent flow and are **regression
guards** for that fix: a second run after removing a protected path must still
succeed, and for a default path protection must be re-established. They were red
before the reconcile landed; they must stay green after it.

Each test uses a distinct ``--slot`` so they never collide (``cleanup_containers``
tears down slots 1-10 for the workspace).
"""

import shutil
import subprocess
import time
from pathlib import Path

from support.helpers import calculate_container_name, write_workspace_container_config


def _first_persistent_run(coi_binary, workspace_dir, slot, marker):
    """Run one persistent ``coi run`` that materializes the default protected
    paths and leaves the container behind (stopped). Returns the container name.
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot",
            str(slot),
            "echo",
            marker,
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    assert result.returncode == 0, f"First persistent run should succeed. stderr: {result.stderr}"
    assert marker in (result.stdout + result.stderr), "First run should echo its marker"

    container_name = calculate_container_name(workspace_dir, slot)
    # The container must have survived the first run (persistent mode).
    exists = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert exists.returncode == 0, "Persistent container should still exist after first run"
    return container_name


def test_restart_after_removing_materialized_dir(coi_binary, cleanup_containers, workspace_dir):
    """#610 core repro (directory device): a materialized default protected dir
    (``.husky``) removed while the persistent container is stopped must not wedge
    the next run.

    Today the second run fails because the ``protect-husky`` device references a
    now-missing source and Incus rejects the container at start
    (``Missing source path ".../.husky"``).
    """
    slot = 3
    write_workspace_container_config(workspace_dir, persistent=True)

    _first_persistent_run(coi_binary, workspace_dir, slot, "first-run-husky")

    # COI materialized .husky on first run; confirm, then delete it to simulate a
    # workspace that no longer has the path (orchestrator recreated it, cleanup, etc).
    husky = Path(workspace_dir) / ".husky"
    assert husky.exists(), ".husky should have been materialized by the first run"
    shutil.rmtree(husky)
    assert not husky.exists()

    time.sleep(1)

    # Second run reuses the stopped persistent container -> stopped-restart branch
    # -> mgr.Start(). It must reconcile the stale protect-husky device and succeed.
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot",
            str(slot),
            "echo",
            "second-run-husky",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    combined = result.stdout + result.stderr
    assert result.returncode == 0, (
        "Second run must not be wedged by a stale protect-* device for a removed "
        f"protected dir. stderr:\n{combined}"
    )
    assert "second-run-husky" in combined, "Second run should execute after reconcile"
    assert "Missing source path" not in combined, (
        f"Incus rejected a stale protect-* device at start:\n{combined}"
    )


def test_restart_after_removing_materialized_file(coi_binary, cleanup_containers, workspace_dir):
    """#610 repro (file device): a materialized default protected *file*
    (``.claude/settings.json``) removed while stopped must not wedge the restart.

    Distinct from the directory case: this device points at a regular-file
    placeholder, exercising the file-type materialization path.
    """
    slot = 4
    write_workspace_container_config(workspace_dir, persistent=True)

    _first_persistent_run(coi_binary, workspace_dir, slot, "first-run-claude")

    settings = Path(workspace_dir) / ".claude" / "settings.json"
    assert settings.exists(), ".claude/settings.json placeholder should have been materialized"
    settings.unlink()
    assert not settings.exists()

    time.sleep(1)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot",
            str(slot),
            "echo",
            "second-run-claude",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    combined = result.stdout + result.stderr
    assert result.returncode == 0, (
        "Second run must not be wedged by a stale protect-* device for a removed "
        f"protected file. stderr:\n{combined}"
    )
    assert "second-run-claude" in combined, "Second run should execute after reconcile"
    assert "Missing source path" not in combined, (
        f"Incus rejected a stale protect-* device at start:\n{combined}"
    )


def test_protection_reestablished_after_restart(coi_binary, cleanup_containers, workspace_dir):
    """#610 drift repro: after removing a protected path and restarting, COI must
    not only start but also RE-materialize and RE-protect the path.

    This pins the full fix contract, not just "does it boot": the reuse path
    currently skips ``SetupSecurityMounts`` entirely, so even if the start
    failure were papered over, protection would silently drift (the path would no
    longer be read-only inside the container).
    """
    slot = 5
    write_workspace_container_config(workspace_dir, persistent=True)

    _first_persistent_run(coi_binary, workspace_dir, slot, "first-run-vscode")

    vscode = Path(workspace_dir) / ".vscode"
    assert vscode.exists(), ".vscode should have been materialized by the first run"
    shutil.rmtree(vscode)
    assert not vscode.exists()

    time.sleep(1)

    # Reuse: must start cleanly...
    restart = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot",
            str(slot),
            "echo",
            "restart-ok",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    combined = restart.stdout + restart.stderr
    assert restart.returncode == 0, (
        f"Restart after removing .vscode must succeed. stderr:\n{combined}"
    )
    assert "restart-ok" in combined

    # ...and re-materialize the protected path on the host.
    assert vscode.exists() and vscode.is_dir(), (
        ".vscode must be re-materialized on the host after restart (protection reconcile)"
    )

    # ...and the re-established protection must actually be read-only in-container:
    # a write from inside must fail and must not persist attacker content.
    attack = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot",
            str(slot),
            "--",
            "sh",
            "-c",
            "echo pwn > /workspace/.vscode/tasks.json",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    attack_out = (attack.stdout + attack.stderr).lower()
    assert attack.returncode != 0, "Write into re-protected .vscode must fail from inside container"
    assert (
        "read-only" in attack_out or "read only" in attack_out or "permission denied" in attack_out
    ), f"Expected a read-only/permission error, got:\n{attack.stdout + attack.stderr}"

    tasks_json = vscode / "tasks.json"
    if tasks_json.exists():
        assert tasks_json.read_text() == "", "Attacker content must not persist to host .vscode"


def test_restart_after_removing_user_additional_protected_path(
    coi_binary, cleanup_containers, workspace_dir
):
    """#610 breadth repro: the reconcile gap is not limited to COI's own default
    paths. A user-declared ``additional_protected_paths`` entry that existed at
    first launch (so a ``protect-*`` device was attached for it) and is later
    removed while stopped wedges the restart exactly the same way.

    This proves the fix must reconcile the whole protect-device class, not just
    the auto-materialized defaults.
    """
    slot = 6

    # Declare .idea as an extra protected path (project scope; conftest sets
    # COI_TRUST_ALL=1 so project-scoped additional_protected_paths is honored).
    coi_dir = Path(workspace_dir) / ".coi"
    coi_dir.mkdir(parents=True, exist_ok=True)
    (coi_dir / "config.toml").write_text('[security]\nadditional_protected_paths = [".idea"]\n')
    write_workspace_container_config(workspace_dir, persistent=True)

    # User-added paths are NOT auto-materialized — it must exist for a device to
    # be attached. Pre-create it so the first run attaches protect-idea.
    idea = Path(workspace_dir) / ".idea"
    idea.mkdir()
    (idea / "workspace.xml").write_text("<project></project>")

    _first_persistent_run(coi_binary, workspace_dir, slot, "first-run-idea")

    shutil.rmtree(idea)
    assert not idea.exists()

    time.sleep(1)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot",
            str(slot),
            "echo",
            "second-idea",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    combined = result.stdout + result.stderr
    assert result.returncode == 0, (
        "Restart after removing a user additional_protected_path must not be wedged "
        f"by its stale protect-* device. stderr:\n{combined}"
    )
    assert "second-idea" in combined
    assert "Missing source path" not in combined, (
        f"Incus rejected a stale protect-* device at start:\n{combined}"
    )
