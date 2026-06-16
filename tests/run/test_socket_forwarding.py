"""
End-to-end tests for host Unix socket forwarding (`[[sockets]]` and the built-in
`[ssh] forward_agent` fast-track).

A forwarded socket is exposed inside the container via an Incus proxy device, so
the host endpoint never enters the container. `[ssh] forward_agent = true` is the
simple fast-track (one built-in socket: /tmp/ssh-agent.sock + SSH_AUTH_SOCK).
Sockets declared by an untrusted project .coi/config.toml are gated behind
`coi trust` exactly like out-of-workspace mounts (a forwarded host socket exposes
a host capability), and the trust fingerprint spans both mounts and sockets.

These tests assert the proxy device materializes the socket inside the container
and the configured env var is injected; byte-level transfer over the same proxy
mechanism is covered by the Go SSH-agent integration test.
"""

import os
import pathlib
import socket
import subprocess
import tempfile

CONTAINER_SOCK = "/tmp/broker.sock"
ENV_NAME = "BROKER_SOCK"


def make_host_socket():
    """Create a listening host Unix socket in a short /tmp dir (path length is
    capped at ~108 bytes, so pytest's deep tmp_path can't be used)."""
    d = tempfile.mkdtemp(prefix="coisock")
    path = os.path.join(d, "broker.sock")
    srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    srv.bind(path)
    srv.listen(8)
    return srv, path


def env_gate_active():
    return {k: v for k, v in os.environ.items() if k != "COI_TRUST_ALL"}


def probe_socket(coi_binary, workspace_dir, env, extra_args=None):
    """Run `coi run` and report, for the forwarded socket, whether it exists in
    the container and what the env var resolves to."""
    args = [coi_binary, "run"]
    if extra_args:
        args += extra_args
    args += [
        "--",
        "sh",
        "-c",
        f"if [ -S {CONTAINER_SOCK} ]; then echo PRESENT; else echo ABSENT; fi; "
        f'echo "ENV=${ENV_NAME}"',
    ]
    return subprocess.run(
        args,
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
        env=env,
    )


def test_ssh_forward_agent_regression(coi_binary, workspace_dir, cleanup_containers, tmp_path):
    """`[ssh] forward_agent = true` still forwards $SSH_AUTH_SOCK to the fixed
    /tmp/ssh-agent.sock and sets SSH_AUTH_SOCK in the container."""
    srv, host_sock = make_host_socket()
    try:
        cfg = tmp_path / "trusted-config.toml"
        cfg.write_text("[ssh]\nforward_agent = true\n")
        env = dict(os.environ)
        env["COI_CONFIG"] = str(cfg)
        env["SSH_AUTH_SOCK"] = host_sock

        r = subprocess.run(
            [
                coi_binary,
                "run",
                "--",
                "sh",
                "-c",
                "if [ -S /tmp/ssh-agent.sock ]; then echo PRESENT; else echo ABSENT; fi; "
                'echo "ENV=$SSH_AUTH_SOCK"',
            ],
            capture_output=True,
            text=True,
            timeout=120,
            cwd=workspace_dir,
            env=env,
        )
        assert "PRESENT" in r.stdout, (
            f"SSH agent socket should be forwarded. out={r.stdout} err={r.stderr}"
        )
        assert "ENV=/tmp/ssh-agent.sock" in r.stdout, f"SSH_AUTH_SOCK should be set. out={r.stdout}"
    finally:
        srv.close()


def test_configured_socket_forwarded_and_env_set(
    coi_binary, workspace_dir, cleanup_containers, tmp_path
):
    """A [[sockets]] entry from a trusted-scope ($COI_CONFIG) config is forwarded
    and its env var is injected."""
    srv, host_sock = make_host_socket()
    try:
        cfg = tmp_path / "trusted-config.toml"
        cfg.write_text(
            f'[[sockets]]\nhost = "{host_sock}"\ncontainer = "{CONTAINER_SOCK}"\nenv = "{ENV_NAME}"\n'
        )
        env = dict(os.environ)
        env["COI_CONFIG"] = str(cfg)

        r = probe_socket(coi_binary, workspace_dir, env)
        assert "PRESENT" in r.stdout, (
            f"configured socket should be forwarded. out={r.stdout} err={r.stderr}"
        )
        assert f"ENV={CONTAINER_SOCK}" in r.stdout, f"{ENV_NAME} should be set. out={r.stdout}"
    finally:
        srv.close()


def test_untrusted_socket_gated_until_trusted(
    coi_binary, workspace_dir, cleanup_containers, tmp_path
):
    """A socket declared by the untrusted project .coi/config.toml is gated until
    `coi trust`, then forwarded; `coi untrust` re-arms the gate."""
    srv, host_sock = make_host_socket()
    try:
        cfg_dir = pathlib.Path(workspace_dir) / ".coi"
        cfg_dir.mkdir(exist_ok=True)
        (cfg_dir / "config.toml").write_text(
            f'[[sockets]]\nhost = "{host_sock}"\ncontainer = "{CONTAINER_SOCK}"\nenv = "{ENV_NAME}"\n'
        )
        env = env_gate_active()

        # 1. Untrusted -> gated: socket absent, env empty, warning emitted.
        r = probe_socket(coi_binary, workspace_dir, env)
        assert "ABSENT" in r.stdout, (
            f"untrusted socket should be gated. out={r.stdout} err={r.stderr}"
        )
        assert "ignoring untrusted socket" in r.stderr.lower(), (
            f"expected the socket gate warning. err={r.stderr}"
        )

        # 2. Approve it.
        t = subprocess.run(
            [coi_binary, "trust"],
            capture_output=True,
            text=True,
            timeout=30,
            cwd=workspace_dir,
            env=env,
        )
        assert t.returncode == 0, f"coi trust should succeed. err={t.stderr}"
        assert "trusted" in (t.stdout + t.stderr).lower(), (
            f"unexpected trust output: {t.stdout}{t.stderr}"
        )

        # 3. Now forwarded.
        r = probe_socket(coi_binary, workspace_dir, env)
        assert "PRESENT" in r.stdout, (
            f"socket should be forwarded after trust. out={r.stdout} err={r.stderr}"
        )
        assert f"ENV={CONTAINER_SOCK}" in r.stdout, f"env should be set after trust. out={r.stdout}"

        # 4. Revoke -> gated again.
        u = subprocess.run(
            [coi_binary, "untrust"],
            capture_output=True,
            text=True,
            timeout=30,
            cwd=workspace_dir,
            env=env,
        )
        assert u.returncode == 0, f"untrust failed: {u.stderr}"
        r = probe_socket(coi_binary, workspace_dir, env)
        assert "ABSENT" in r.stdout, f"socket must be gated again after untrust. out={r.stdout}"
    finally:
        srv.close()


def test_untrusted_socket_trust_all_bypass(coi_binary, workspace_dir, cleanup_containers, tmp_path):
    """COI_TRUST_ALL=1 bypasses the socket gate for CI/automation."""
    srv, host_sock = make_host_socket()
    try:
        cfg_dir = pathlib.Path(workspace_dir) / ".coi"
        cfg_dir.mkdir(exist_ok=True)
        (cfg_dir / "config.toml").write_text(
            f'[[sockets]]\nhost = "{host_sock}"\ncontainer = "{CONTAINER_SOCK}"\nenv = "{ENV_NAME}"\n'
        )
        env = dict(os.environ)
        env["COI_TRUST_ALL"] = "1"

        r = probe_socket(coi_binary, workspace_dir, env)
        assert "PRESENT" in r.stdout, (
            f"COI_TRUST_ALL=1 should bypass the socket gate. out={r.stdout} err={r.stderr}"
        )
    finally:
        srv.close()
