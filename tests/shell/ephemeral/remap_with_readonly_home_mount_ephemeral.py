"""coi shell with a code_uid remap + a read-only mount under /home/code (PR #608).

A non-default ``[incus] code_uid`` makes coi remap the container's ``code`` user
at setup time. Disk devices attach pre-start (#534), so a user-declared
read-only ``[[mounts.default]]`` entry under ``/home/code`` is already live when
the remap runs. Two failure modes used to abort the whole session even though
the UID/GID change itself had been committed:

1. The setup chain fused ``groupmod && usermod && chown -R`` into one fatal
   command — the trailing ``chown -R`` hits ``Read-only file system`` on the
   mount (after fixing everything it could) and the non-zero exit killed setup.
2. ``usermod -u`` itself, even without ``-m``, walks the home directory chowning
   files owned by the old UID; hitting the read-only mount makes it exit
   E_HOMEDIR *after* the passwd update was committed.

The fix probes the account's actual UID before treating a remap exit code as
fatal, and runs the ownership sweep separately best-effort (mirroring coi run).

This test mounts a read-only host dir at /home/code/roshare with a file owned by
UID 1000 (the image's default code UID), so BOTH failure modes trigger, and
asserts the session still comes up with the remapped UID in effect.
"""

import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    get_container_list,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
    write_trusted_coi_config,
)

# Non-default on purpose: triggers the remap (image default is 1000). 1001 is
# also the CI runner's UID, so no raw.idmap subuid surprises on the runner.
REMAP_UID = 1001


def test_remap_with_readonly_home_mount(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    # Host dir mounted read-only under the code user's home.
    roshare = tmp_path / "roshare"
    roshare.mkdir()
    marker = roshare / "plugin.txt"
    marker.write_text("host-shared plugin data\n")

    # Own the file as the OLD code UID (1000) so usermod's internal home walk
    # also trips on it (failure mode 2), not just the explicit chown -R
    # (failure mode 1, which triggers on any read-only mount regardless of
    # ownership). Best-effort: without sudo the test still exercises mode 1.
    subprocess.run(
        ["sudo", "-n", "chown", "1000:1000", str(marker)],
        capture_output=True,
        timeout=10,
        check=False,
    )

    env = write_trusted_coi_config(
        f"""
[network]
mode = "open"

[incus]
code_uid = {REMAP_UID}

[[mounts.default]]
host = "{roshare}"
container = "/home/code/roshare"
readonly = true
"""
    )
    env["COI_USE_DUMMY"] = "1"
    container_name = calculate_container_name(workspace_dir, 1)

    child = spawn_coi(
        coi_binary,
        ["shell", f"--workspace={workspace_dir}"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    # THE regression assertion: pre-fix, setup aborts here with
    # "failed to remap user code to UID 1001: ... Read-only file system"
    # and the session never becomes ready.
    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    # Exit the CLI to bash to verify the remap actually landed (the tolerance
    # must not mask a remap that never happened).
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(5)

    with with_live_screen(child) as monitor:
        time.sleep(1)
        # Command substitution keeps the numeric sentinel out of the echoed
        # command line (it shows the unexpanded $(...)), so the monitor can't
        # false-match on the echo itself.
        child.send("echo CODE_UID_IS_$(id -u code)_END")
        time.sleep(0.5)
        child.send("\x0d")
        time.sleep(2)
        uid_ok = wait_for_text_in_monitor(monitor, f"CODE_UID_IS_{REMAP_UID}_END", timeout=20)

    # The read-only mount must be visible too (it's the ingredient that used to
    # break setup — prove it's actually there, so the test can't silently pass
    # because the mount was dropped).
    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("ls /home/code/roshare/")
        time.sleep(0.5)
        child.send("\x0d")
        time.sleep(2)
        mount_ok = wait_for_text_in_monitor(monitor, "plugin.txt", timeout=20)

    # Cleanup
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    try:
        child.expect(EOF, timeout=30)
    except TIMEOUT:
        pass
    try:
        child.close(force=False)
    except Exception:
        child.close(force=True)
    time.sleep(3)

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
    time.sleep(1)
    assert container_name not in get_container_list(), (
        f"Container {container_name} should be deleted after cleanup"
    )

    assert uid_ok, (
        f"code user should report UID {REMAP_UID} after remap — the read-only-mount "
        "tolerance must not mask a remap that never applied"
    )
    assert mount_ok, "read-only mount under /home/code should be present and readable"
