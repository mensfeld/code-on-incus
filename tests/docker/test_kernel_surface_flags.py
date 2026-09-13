"""
Tests for the kernel attack-surface hardening flags:
[container] docker and [security] reduce_kernel_surface.

These assert the instance config `coi container launch` applies — the same
robust path test_docker_flags_enabled.py uses — rather than a full `coi run`
launch, so they don't depend on the nested-idmap container START that CI's
container lane cannot always complete. What the flags CONTROL is the incus
config; a real boot+smoke under the deny list is covered by Go-level tests and
manual verification.

Covered:
1. [container] docker = false (a tightening, honored from project scope)
   launches with none of the four Docker-support keys.
2. [security] reduce_kernel_surface = true (trusted scope) disables Docker
   support AND sets security.syscalls.deny to the full deny list.
3. An untrusted project config's docker = true cannot re-enable nesting that
   trusted config disabled.
4. [security] reduce_kernel_surface_strict = true (trusted scope) implies the
   base tier AND adds perf_event_open to security.syscalls.deny.
"""

import os
import subprocess

DENY_SYSCALLS = [
    "io_uring_setup",
    "io_uring_enter",
    "io_uring_register",
    "bpf",
    "userfaultfd",
    "keyctl",
    "add_key",
    "request_key",
]

DOCKER_KEYS = [
    "security.nesting",
    "security.syscalls.intercept.mknod",
    "security.syscalls.intercept.setxattr",
    "linux.sysctl.net.ipv4.ip_unprivileged_port_start",
]


def incus_config_get(container_name, key):
    result = subprocess.run(
        ["incus", "--project", "default", "config", "get", container_name, key],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"incus config get {key} failed: {result.stderr}"
    return result.stdout.strip()


def launch(coi_binary, name, env=None, cwd=None):
    """coi container launch applies the kernel-surface policy at init, before
    the container start that CI's nested environment may not complete — so the
    config assertions hold regardless of whether the start itself succeeds."""
    return subprocess.run(
        [coi_binary, "container", "launch", "coi-default", name],
        capture_output=True,
        text=True,
        timeout=120,
        env=env,
        cwd=cwd,
    )


def write_project_config(workspace_dir, content):
    coi_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(coi_dir, exist_ok=True)
    with open(os.path.join(coi_dir, "config.toml"), "w") as f:
        f.write(content)


def test_docker_disabled_via_project_config(coi_binary, cleanup_containers, workspace_dir):
    """[container] docker = false is a tightening, so a project config may set
    it; the container then carries none of the Docker-support keys and no
    syscall deny list."""
    write_project_config(workspace_dir, "[container]\ndocker = false\n")
    name = "coi-ks-docker-off"
    try:
        launch(coi_binary, name, cwd=workspace_dir)
        for key in DOCKER_KEYS:
            assert incus_config_get(name, key) in ("", "false"), (
                f"{key} should be unset with docker=false"
            )
        assert incus_config_get(name, "security.syscalls.deny") == "", (
            "deny list should stay unset without reduce_kernel_surface"
        )
    finally:
        subprocess.run(["incus", "--project", "default", "delete", name, "--force"], timeout=60)


def test_reduce_kernel_surface_hardening(coi_binary, cleanup_containers, tmp_path):
    """Trusted [security] reduce_kernel_surface = true disables Docker support
    and installs the full syscall deny list."""
    cfg = tmp_path / "trusted.toml"
    cfg.write_text("[security]\nreduce_kernel_surface = true\n")
    env = {**os.environ, "COI_CONFIG": str(cfg)}
    name = "coi-ks-hardened"
    try:
        launch(coi_binary, name, env=env)
        for key in DOCKER_KEYS:
            assert incus_config_get(name, key) in ("", "false"), (
                f"{key} should be unset under reduce_kernel_surface"
            )
        deny = incus_config_get(name, "security.syscalls.deny").split()
        for syscall in DENY_SYSCALLS:
            assert syscall in deny, f"{syscall} missing from deny list: {deny}"
    finally:
        subprocess.run(["incus", "--project", "default", "delete", name, "--force"], timeout=60)


def test_reduce_kernel_surface_strict_adds_perf_event_open(
    coi_binary, cleanup_containers, tmp_path
):
    """Trusted [security] reduce_kernel_surface_strict = true implies the base
    tier (Docker off + full base deny list) and additionally denies
    perf_event_open — the one syscall the strict tier adds."""
    cfg = tmp_path / "trusted.toml"
    cfg.write_text("[security]\nreduce_kernel_surface_strict = true\n")
    env = {**os.environ, "COI_CONFIG": str(cfg)}
    name = "coi-ks-strict"
    try:
        launch(coi_binary, name, env=env)
        for key in DOCKER_KEYS:
            assert incus_config_get(name, key) in ("", "false"), (
                f"{key} should be unset under reduce_kernel_surface_strict"
            )
        deny = incus_config_get(name, "security.syscalls.deny").split()
        for syscall in DENY_SYSCALLS:
            assert syscall in deny, f"base {syscall} missing under strict tier: {deny}"
        assert "perf_event_open" in deny, (
            f"strict tier must add perf_event_open to the deny list: {deny}"
        )
    finally:
        subprocess.run(["incus", "--project", "default", "delete", name, "--force"], timeout=60)


def test_untrusted_docker_reenable_ignored(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """A project config's docker = true must not re-enable nesting that trusted
    config disabled — an untrusted repo must never widen the kernel surface."""
    cfg = tmp_path / "trusted.toml"
    cfg.write_text("[container]\ndocker = false\n")
    env = {**os.environ, "COI_CONFIG": str(cfg)}
    write_project_config(workspace_dir, "[container]\ndocker = true\n")
    name = "coi-ks-untrusted"
    try:
        result = launch(coi_binary, name, env=env, cwd=workspace_dir)
        assert "container.docker" in result.stderr, (
            f"expected the untrusted-downgrade warning naming container.docker; stderr:\n{result.stderr}"
        )
        assert incus_config_get(name, "security.nesting") in ("", "false"), (
            "untrusted docker=true must not re-enable security.nesting"
        )
    finally:
        subprocess.run(["incus", "--project", "default", "delete", name, "--force"], timeout=60)
