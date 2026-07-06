"""
Issue #534: a workspace on a virtiofs mount (macOS Colima/Lima/OrbStack sharing
the Mac filesystem into the Linux VM) must work with `coi run`.

Previously `coi run` hot-mounted the workspace into the already-running,
already-isolated container; virtiofs cannot satisfy security.idmap.isolated's
idmapped-mount requirement, so the hot-add failed hard:

    Error: Failed to start device "workspace": Required idmapping abilities not available

The fix attaches all disk devices BEFORE first start (the shell path always did),
so an idmap-incompatible device filesystem fails the START — where the existing
isolation fallback disables security.idmap.isolated and retries — instead of
failing a post-start hot-mount that has no fallback at all.

This test only runs where a writable virtiofs directory is available, exported
as COI_TEST_VIRTIOFS_DIR — in CI that is the lima-smoke lane (the Lima guest's
mounted host $HOME). On native runners (no virtiofs) it skips.
"""

import os
import shutil
import subprocess
import tempfile

import pytest

from support.helpers import calculate_container_name


def _virtiofs_dir_or_skip():
    """Return the configured virtiofs directory, or skip if unavailable/unfit."""
    vdir = os.environ.get("COI_TEST_VIRTIOFS_DIR")
    if not vdir:
        pytest.skip("COI_TEST_VIRTIOFS_DIR not set (no virtiofs mount to test against)")
    if not os.path.isdir(vdir):
        pytest.skip(f"COI_TEST_VIRTIOFS_DIR={vdir} is not a directory")
    # The directory must actually be on virtiofs — guard against a misconfigured
    # env var silently degrading this into a plain native-fs test. The match is
    # path-boundary-aware: in the Lima guest, the NATIVE home /home/runner.guest
    # startswith the virtiofs mount point /home/runner — a bare prefix match
    # would wave exactly the wrong directory through.
    try:
        with open("/proc/mounts") as f:
            mounts = f.read()
    except OSError:
        pytest.skip("cannot read /proc/mounts")

    def _under(path, mount_point):
        return path == mount_point or path.startswith(mount_point.rstrip("/") + "/")

    on_virtiofs = any(
        len(parts) > 2 and parts[2] == "virtiofs" and _under(vdir, parts[1])
        for parts in (line.split() for line in mounts.splitlines())
    )
    if not on_virtiofs:
        pytest.skip(f"COI_TEST_VIRTIOFS_DIR={vdir} is not on a virtiofs mount")
    return vdir


def test_run_works_with_virtiofs_workspace(coi_binary):
    """`coi run` against a virtiofs-backed workspace succeeds end-to-end (#534):
    the write lands on the (virtiofs) host mount and the in-container writer is
    the code user. If isolation had to be disabled to get there, that is the
    documented fallback — success is the contract, isolation is best-effort."""
    vdir = _virtiofs_dir_or_skip()
    workspace = tempfile.mkdtemp(prefix="coi-534-ws-", dir=vdir)
    try:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace,
                "--",
                "bash",
                "-c",
                "id -u > /workspace/uid.txt && echo virtiofs-write-ok > /workspace/marker.txt",
            ],
            capture_output=True,
            text=True,
            # Generous: the isolation fallback path may recreate the container
            # (start fails -> ephemeral recreate without isolation -> start).
            timeout=300,
        )
        combined = result.stdout + result.stderr
        assert result.returncode == 0, (
            f"coi run must succeed on a virtiofs-backed workspace (issue #534). Output:\n{combined}"
        )
        # The old failure mode must not appear anywhere, even as a warning that
        # was survived — its presence with rc=0 would mean a silent half-mount.
        assert "Required idmapping abilities not available" not in combined or (
            "isolation not" in combined.lower() or "retrying" in combined.lower()
        ), f"idmap failure without the documented fallback. Output:\n{combined}"

        marker = os.path.join(workspace, "marker.txt")
        assert os.path.exists(marker), (
            f"the container's write must be visible on the virtiofs host mount. Output:\n{combined}"
        )
        with open(marker) as f:
            assert f.read().strip() == "virtiofs-write-ok"
        with open(os.path.join(workspace, "uid.txt")) as f:
            assert f.read().strip() == "1000", (
                "the writer inside the container should be code (uid 1000)"
            )
    finally:
        # This workspace is NOT the cleanup_containers fixture's workspace_dir,
        # so that fixture cannot compute this test's container names. If coi was
        # SIGKILLed (subprocess timeout) its own teardown never ran — force-kill
        # the slots for THIS workspace so a leaked container doesn't outlive the
        # test (and doesn't pin the virtiofs tmpdir against rmtree).
        for slot in range(1, 11):
            subprocess.run(
                ["incus", "delete", calculate_container_name(workspace, slot), "--force"],
                capture_output=True,
                check=False,
            )
        shutil.rmtree(workspace, ignore_errors=True)
