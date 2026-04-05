"""
Meta test for the Makefile `check-deps` preflight target.

Regression test: when building from source on a fresh Ubuntu box without
`libsystemd-dev` (or `pkg-config`) installed, `make build` used to crash
with a cryptic cgo/pkg-config error. The `check-deps` target now runs
before `build` and prints an actionable install hint instead.

This test validates the behaviour by:
1. Launching a fresh Ubuntu 24.04 container with only `make` installed.
2. Pushing the repo's Makefile in.
3. Running `make check-deps` — expected to fail with a clear message
   naming the missing dependency and giving apt/dnf/pacman install commands.
4. Installing `pkg-config` + `libsystemd-dev`.
5. Running `make check-deps` again — expected to succeed.

macOS note: the `check-deps` target is a no-op on Darwin (guarded by
`uname -s = Linux`), because the only cgo consumer
(`internal/nftmonitor/journalctl.go`) is gated on `//go:build linux` and
therefore never links `libsystemd` on macOS. That path is verified by
inspection rather than in this integration test (we can't run a macOS
VM under Incus on Linux CI).
"""

import os
import subprocess
import time

import pytest


REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
MAKEFILE_PATH = os.path.join(REPO_ROOT, "Makefile")


@pytest.fixture(scope="module")
def bare_container():
    """Launch a fresh Ubuntu 24.04 container with no build deps installed."""
    container_name = "coi-check-deps-test"

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
        pytest.skip(f"Failed to launch check-deps test container: {result.stderr}")

    # Wait for container networking to come up.
    time.sleep(10)

    yield container_name

    subprocess.run(
        ["incus", "delete", container_name, "--force"],
        capture_output=True,
        check=False,
    )


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


def test_check_deps_reports_missing_libsystemd(bare_container):
    """
    `make check-deps` should fail with a friendly, actionable message when
    `pkg-config`/`libsystemd-dev` is missing, and succeed once they are
    installed.
    """
    container_name = bare_container

    # Install only `make` so we can invoke the target. Deliberately skip
    # pkg-config and libsystemd-dev — they are the subject of the test.
    _apt_install(container_name, "make")

    # Push the current repo's Makefile into the container. `check-deps` is
    # self-contained and does not depend on any other file in the tree.
    push = subprocess.run(
        ["incus", "file", "push", MAKEFILE_PATH, f"{container_name}/root/Makefile"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert push.returncode == 0, f"Failed to push Makefile: {push.stderr}"

    # Step 1: pkg-config itself is missing. check-deps must fail loudly.
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
    assert "libsystemd-dev" in combined, (
        f"check-deps did not mention libsystemd-dev.\n"
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
