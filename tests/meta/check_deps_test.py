"""
Meta tests for the Makefile `check-deps` preflight target and the
build-from-source flow.

Regression context: on a fresh Ubuntu box, `make build` used to crash with
a cryptic `go: not found` (when Go was absent — for example under sudo,
which strips PATH and hides user-scoped Go installs like mise) or with an
opaque cgo/pkg-config error (when `libsystemd-dev` was missing). The
`check-deps` target now runs before `build` and prints an actionable
install hint for each missing dependency instead.

Two independent integration tests cover this, each in its own container so
they are order-independent under pytest-randomly:

1. `test_check_deps_reports_missing_system_deps` — install a no-op `go`
   stub first (so the Go check passes) and walk through the system-deps
   layers: pkg-config missing → friendly error, libsystemd-dev missing →
   friendly error, both present → success. Exercises `make check-deps`
   directly.

2. `test_make_build_fails_gracefully_when_go_missing` — leave Go absent
   and invoke the actual compile-from-source entrypoint (`make build`).
   Asserts the friendly Go error is printed (with tarball / apt / dnf /
   pacman / mise hints and the sudo-strips-PATH note) and that the raw
   `go: not found` error is not what the user sees.

macOS note: the `check-deps` target's libsystemd branch is a no-op on
Darwin (guarded by `uname -s = Linux`), because the only cgo consumer
(`internal/nftmonitor/journalctl.go`) is gated on `//go:build linux` and
therefore never links `libsystemd` on macOS. That path is verified by
inspection rather than in this integration test (we can't run a macOS VM
under Incus on Linux CI).
"""

import os
import subprocess
import time

import pytest

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
MAKEFILE_PATH = os.path.join(REPO_ROOT, "Makefile")


def _exec(container_name, command, timeout=300, check=False):
    return subprocess.run(
        ["incus", "exec", container_name, "--", "bash", "-c", command],
        capture_output=True,
        text=True,
        timeout=timeout,
        check=check,
    )


def _apt_install(container_name, packages):
    """Install apt packages with a small retry loop for flaky CI networks."""
    cmd = (
        "set -e; "
        "for i in 1 2 3; do apt-get update -qq && break || sleep 5; done; "
        f"DEBIAN_FRONTEND=noninteractive apt-get install -y -qq {packages}"
    )
    return _exec(container_name, cmd, timeout=600, check=True)


def _launch_bare_container(container_name):
    """Launch a fresh Ubuntu 24.04 container with no build deps installed."""
    subprocess.run(
        ["incus", "delete", container_name, "--force"],
        capture_output=True,
        check=False,
    )

    result = subprocess.run(
        ["incus", "launch", "images:ubuntu/24.04", container_name],
        capture_output=True,
        text=True,
        timeout=180,
    )
    if result.returncode != 0:
        pytest.skip(f"Failed to launch container {container_name}: {result.stderr}")

    # Wait for container networking to come up.
    time.sleep(10)
    return container_name


def _delete_container(container_name):
    subprocess.run(
        ["incus", "delete", container_name, "--force"],
        capture_output=True,
        check=False,
    )


def _push_makefile(container_name):
    push = subprocess.run(
        ["incus", "file", "push", MAKEFILE_PATH, f"{container_name}/root/Makefile"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert push.returncode == 0, f"Failed to push Makefile: {push.stderr}"


@pytest.fixture(scope="module")
def system_deps_container():
    name = _launch_bare_container("coi-check-deps-system")
    yield name
    _delete_container(name)


@pytest.fixture(scope="module")
def missing_go_container():
    name = _launch_bare_container("coi-check-deps-nogo")
    yield name
    _delete_container(name)


def test_check_deps_reports_missing_system_deps(system_deps_container):
    """
    `make check-deps` should fail with a friendly, actionable message for
    each missing system build dependency (pkg-config, then libsystemd
    headers) and succeed once they are all present.

    This test installs a no-op `go` stub so the Go check passes and we can
    exercise the Linux-specific system-deps branch. The separate test
    `test_make_build_fails_gracefully_when_go_missing` covers the Go
    branch.
    """
    container_name = system_deps_container

    # Install `make` so we can invoke the target. Deliberately skip
    # pkg-config and libsystemd-dev — they are the subject of the test.
    _apt_install(container_name, "make")

    # Drop a no-op `go` on PATH so the Go check passes. `check-deps` only
    # calls `command -v go`, so the stub never actually has to run.
    _exec(
        container_name,
        "set -e; printf '#!/bin/sh\\nexit 0\\n' > /usr/local/bin/go; chmod +x /usr/local/bin/go",
        check=True,
    )

    _push_makefile(container_name)

    # Step 1: pkg-config is missing. check-deps must fail loudly.
    result = _exec(container_name, "cd /root && make check-deps")
    assert result.returncode != 0, (
        "check-deps unexpectedly succeeded without pkg-config/libsystemd-dev"
    )
    combined = result.stdout + result.stderr
    assert "missing build dependency" in combined, (
        f"check-deps did not print the expected header.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
    assert "pkg-config" in combined, "check-deps did not mention pkg-config"
    assert "sudo apt install -y pkg-config libsystemd-dev" in combined, (
        "check-deps did not print the Ubuntu/Debian install hint"
    )
    # Sanity: the cryptic raw cgo/pkg-config error should not be what the
    # user sees first.
    assert "Package libsystemd was not found" not in combined, (
        "check-deps leaked the raw pkg-config error instead of the friendly message"
    )

    # Step 2: install pkg-config alone — libsystemd headers still missing.
    _apt_install(container_name, "pkg-config")
    result = _exec(container_name, "cd /root && make check-deps")
    assert result.returncode != 0, (
        "check-deps unexpectedly succeeded with pkg-config but no libsystemd-dev"
    )
    combined = result.stdout + result.stderr
    assert "libsystemd development headers" in combined, (
        f"check-deps did not mention libsystemd development headers.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
    assert "sudo apt install -y pkg-config libsystemd-dev" in combined

    # Step 3: install libsystemd-dev — check-deps should now succeed silently.
    _apt_install(container_name, "libsystemd-dev")
    result = _exec(container_name, "cd /root && make check-deps")
    assert result.returncode == 0, (
        f"check-deps failed with all dependencies installed.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )


def test_make_build_fails_gracefully_when_go_missing(missing_go_container):
    """
    When Go is missing, the compile-from-source entrypoint (`make build`)
    must fail fast with an actionable message rather than the cryptic raw
    `go: not found` error. This mirrors the scenario users hit when
    install.sh runs the build-from-source path on a machine where Go is
    not installed (or where it is user-scoped and sudo has stripped PATH).
    """
    container_name = missing_go_container

    # Only `make` — Go is deliberately absent. We don't install pkg-config /
    # libsystemd-dev either, but check-deps checks Go first so they are
    # irrelevant to the assertion.
    _apt_install(container_name, "make")
    _push_makefile(container_name)

    # Confirm there really is no `go` in PATH — otherwise the test would
    # silently validate nothing.
    probe = _exec(container_name, "command -v go || echo __no_go__")
    assert "__no_go__" in probe.stdout, (
        f"Unexpected go in PATH inside bare container: {probe.stdout!r}"
    )

    # Invoke the real compile-from-source entrypoint.
    result = _exec(container_name, "cd /root && make build")
    assert result.returncode != 0, "make build unexpectedly succeeded without Go"

    combined = result.stdout + result.stderr
    assert "Go toolchain not found" in combined, (
        f"make build did not print the friendly Go error header.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
    assert "https://go.dev/doc/install" in combined, (
        "friendly Go error did not include the official install URL"
    )
    assert "mise use -g go@latest" in combined, (
        "friendly Go error did not mention mise as an install option"
    )
    assert "sudo strips PATH" in combined, (
        "friendly Go error did not warn about the sudo-strips-PATH pitfall"
    )
    # The raw shell error from a missing `go` binary must not be the first
    # thing the user sees.
    assert "go: not found" not in combined, (
        "make build leaked the raw 'go: not found' error instead of the friendly message"
    )
    assert "/bin/sh: 1: go:" not in combined, (
        "make build leaked the raw '/bin/sh: 1: go: ...' error"
    )
