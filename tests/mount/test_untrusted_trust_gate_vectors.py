"""
Trust-gate e2e coverage for the non-mount untrusted vectors (socket, port, credential).

`test_untrusted_mount_trust.py` proves the MOUNT vector is gated when a project
`.coi/config.toml` is untrusted; the socket / port / credential vectors were only
happy-path tested. conftest sets `COI_TRUST_ALL=1` suite-wide, so a naive test of
"the gate blocks X" passes vacuously — a whole category could regress un-noticed.

These run coi with `COI_TRUST_ALL` removed so the gate is active, and assert it
BLOCKS each vector end to end: the config-load -> mark-untrusted -> FilterTrusted
-> not-applied wiring. (The FilterTrusted logic itself is unit-tested in
internal/session/trust_test.go; this covers the CLI wiring around it.)

`coi run` gates mounts/sockets/ports; credentials are gated in `coi shell`'s
session.Setup, so that vector uses a shell probe.
"""

import os
import pathlib
import socket
import subprocess
import tempfile
import time

SENTINEL = "SENTINEL_TRUSTGATE_9f31"


def env_gate_active():
    """Copy of the environment with COI_TRUST_ALL removed, so the gate is armed."""
    return {k: v for k, v in os.environ.items() if k != "COI_TRUST_ALL"}


def write_project_config(workspace_dir, body):
    d = pathlib.Path(workspace_dir) / ".coi"
    d.mkdir(exist_ok=True)
    (d / "config.toml").write_text(body)


# ── socket ────────────────────────────────────────────────────────────────────


def test_untrusted_socket_gated(coi_binary, workspace_dir, cleanup_containers):
    """An untrusted [[sockets]] entry is not forwarded, and a warning is emitted."""
    # A real bound host socket (not served — the gate filters it before any
    # forward). Short dir: AF_UNIX paths are capped at ~108 bytes.
    sockdir = tempfile.mkdtemp(prefix="coi-tg-")
    sock_path = os.path.join(sockdir, "h.sock")
    srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    srv.bind(sock_path)
    srv.listen(1)
    try:
        write_project_config(
            workspace_dir,
            f'[[sockets]]\nhost = "{sock_path}"\ncontainer = "/tmp/gated.sock"\nenv = "GATED_SOCK"\n',
        )
        r = subprocess.run(
            [
                coi_binary,
                "run",
                "--",
                "sh",
                "-c",
                "test -S /tmp/gated.sock && echo SOCK_PRESENT || echo SOCK_ABSENT",
            ],
            capture_output=True,
            text=True,
            timeout=120,
            cwd=workspace_dir,
            env=env_gate_active(),
        )
        assert "ignoring untrusted socket" in r.stderr.lower(), (
            f"expected the socket gate warning on stderr. stderr:\n{r.stderr}"
        )
        assert "SOCK_PRESENT" not in r.stdout, (
            f"an untrusted socket must NOT be forwarded into the container. stdout: {r.stdout}"
        )
    finally:
        srv.close()
        for cleanup in (lambda: os.unlink(sock_path), lambda: os.rmdir(sockdir)):
            try:
                cleanup()
            except OSError:
                pass


# ── port ────────────────────────────────────────────────────────────────────


def test_untrusted_port_gated(coi_binary, workspace_dir, cleanup_containers):
    """An untrusted [[ports.map]] publication is not applied, and a warning is emitted."""
    write_project_config(workspace_dir, '[[ports.map]]\nname = "gated"\ncontainer = 3000\n')
    r = subprocess.run(
        [coi_binary, "run", "--", "sh", "-c", "echo PORTVAR=[${COI_PORT_GATED:-unset}]"],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
        env=env_gate_active(),
    )
    stderr = r.stderr.lower()
    assert "ignoring untrusted" in stderr and "localhost port" in stderr, (
        f"expected the port gate warning on stderr. stderr:\n{r.stderr}"
    )
    # A gated publication does not export COI_PORT_<NAME> into the container.
    assert "PORTVAR=[unset]" in r.stdout, (
        f"an untrusted published port must NOT take effect (COI_PORT_GATED set). stdout: {r.stdout}"
    )


# ── credential (coi shell path) ─────────────────────────────────────────────


def test_untrusted_credential_gated(coi_binary, workspace_dir, cleanup_containers, tmp_path):
    """An untrusted ad-hoc [[credentials]] entry is gated during `coi shell` setup.

    `coi run` does not process credentials (only mounts/sockets/ports), so the
    credential gate lives in `coi shell`'s session.Setup. We start a shell with the
    gate armed, capture its setup output, and assert the credential warning appears.
    """
    cred = tmp_path / "secret.txt"
    cred.write_text(SENTINEL)
    write_project_config(
        workspace_dir,
        f'[[credentials]]\nhost = "{cred}"\ncontainer = "/tmp/gated-cred"\n',
    )

    log = tmp_path / "shell.log"
    with open(log, "w") as fh:
        # Merge stdout+stderr so we catch the setup warning regardless of stream.
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(workspace_dir), "--slot", "1"],
            stdin=subprocess.DEVNULL,
            stdout=fh,
            stderr=subprocess.STDOUT,
            cwd=workspace_dir,
            env=env_gate_active(),
        )
    try:
        found = False
        for _ in range(90):
            if "ignoring untrusted credential" in log.read_text().lower():
                found = True
                break
            time.sleep(1)
        assert found, (
            "expected the credential gate warning during `coi shell` setup. "
            f"output so far:\n{log.read_text()}"
        )
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            proc.kill()
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                pass
