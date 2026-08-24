"""
Meta test for install.sh's ensure_incus_initialized() against a REAL Incus
daemon (#703).

Context: the stubbed Go tests in internal/installer/install_sh_test.go drive
ensure_incus_initialized with a fake `incus` binary, so they can only assert
what we *assume* real `incus` output looks like. #703 was exactly a wrong
assumption about that output: the function treated any non-empty
`incus network list` as "already initialized", but on a real host that list is
never empty (unmanaged physical/loopback interfaces), so init was skipped on
fresh installs and the default profile was left deviceless.

This test closes that gap by running the ACTUAL install.sh logic against a
genuine, freshly-installed-but-not-`admin init`'d Incus daemon inside a nested
container. It validates the three real-world facts the Go stubs cannot:

  1. A fresh daemon's `incus network list --format=csv` is non-empty but has no
     MANAGED=YES row  -> ensure_incus_initialized must decide to run init.
  2. A real storage pool makes `incus storage list --format=csv` non-empty ->
     ensure_incus_initialized must decide to SKIP init (the mitigation added on
     top of the #703 fix: a host with a custom network and no managed bridge is
     still initialized, and its pool is the reliable signal).
  3. A real MANAGED network renders column 3 of the CSV as the literal `YES` ->
     ensure_incus_initialized must decide to SKIP init.

Like tests/installer/zfs_gate_arch_e2e.sh, the privileged action is intercepted
by a RECORDING `sudo` shim: we assert the *decision* install.sh makes (did it
reach for `incus admin init --auto`?) driven by real `incus` reads, rather than
actually running a nested `admin init` (whose bridge/pool creation is flaky
under nesting). The `incus` reads themselves are 100% real.

Requires a host `incus` and nested-Incus support; skips cleanly otherwise.
"""

import os
import subprocess
import tempfile

import pytest

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
INSTALL_SH = os.path.join(REPO_ROOT, "install.sh")

# Drops a recording `sudo` shim ahead of the real PATH (so real `incus` still
# resolves for the reads), sources the real install.sh minus its entrypoint/ERR
# trap, runs one detection cycle, then prints the exit code and everything the
# shim recorded. install.sh's `incus admin init --auto` therefore never runs for
# real -- we only observe whether the function tried to.
HARNESS_SH = r"""#!/usr/bin/env bash
set -uo pipefail
shim="$(mktemp -d)"
export SUDO_LOG="$shim/sudo.log"
: > "$SUDO_LOG"
cat > "$shim/sudo" <<'SHIM'
#!/usr/bin/env bash
echo "$*" >> "$SUDO_LOG"
exit 0
SHIM
chmod +x "$shim/sudo"
export PATH="$shim:$PATH"
export NONINTERACTIVE=1
# Source the real installer without its `main "$@"` entrypoint or ERR trap, the
# same idiom the Go tests and zfs_gate_arch_e2e.sh use.
# shellcheck disable=SC1090
source <(sed '/^main "$@"/d; /^trap error_handler ERR/d' /root/install.sh)
set +e  # the function returns non-zero on soft failures; we assert on output
ensure_incus_initialized
echo "===RC=$?==="
echo "===SUDOLOG_BEGIN==="
cat "$SUDO_LOG"
echo "===SUDOLOG_END==="
"""


def _exec(container, command, timeout=300, check=False):
    return subprocess.run(
        ["incus", "exec", container, "--", "bash", "-c", command],
        capture_output=True,
        text=True,
        timeout=timeout,
        check=check,
    )


def _sudolog(output):
    """Extract the recorded sudo invocations from a harness run's stdout."""
    if "===SUDOLOG_BEGIN===" not in output or "===SUDOLOG_END===" not in output:
        return ""
    return output.split("===SUDOLOG_BEGIN===", 1)[1].split("===SUDOLOG_END===", 1)[0]


@pytest.fixture(scope="module")
def incus_init_container():
    """Nested Ubuntu container running a fresh, UN-initialized Incus daemon.

    We install Incus but deliberately never run `incus admin init`, reproducing
    the fresh-host state from #703. Skips (rather than fails) whenever the
    environment can't provide a nested daemon -- missing host incus, no nesting
    support, apt failures, or a daemon that won't come ready.
    """
    if subprocess.run(["which", "incus"], capture_output=True).returncode != 0:
        pytest.skip("host `incus` not found; cannot launch nested test container")

    name = "coi-incus-init-detection"
    subprocess.run(["incus", "delete", name, "--force"], capture_output=True, check=False)

    launch = subprocess.run(
        ["incus", "launch", "images:ubuntu/24.04", name, "-c", "security.nesting=true"],
        capture_output=True,
        text=True,
        timeout=180,
    )
    if launch.returncode != 0:
        pytest.skip(f"failed to launch nested container: {launch.stderr}")

    try:
        # Wait for networking, then install Incus inside the container.
        install = _exec(
            name,
            """
            set -e
            for i in $(seq 1 30); do
                ping -c1 archive.ubuntu.com >/dev/null 2>&1 && break
                sleep 1
            done
            for i in 1 2 3; do apt-get update -qq && break || sleep 5; done
            DEBIAN_FRONTEND=noninteractive apt-get install -y -qq incus
            systemctl start incus || true
            # Trigger socket activation and wait for the daemon to accept the API.
            incus admin waitready --timeout=60
            """,
            timeout=600,
        )
        if install.returncode != 0:
            pytest.skip(
                "nested Incus daemon unavailable "
                f"(install/waitready failed): {install.stdout}\n{install.stderr}"
            )

        # Sanity: the daemon must genuinely be un-initialized, or the whole
        # premise is void. A fresh daemon has no storage pool yet.
        pools = _exec(name, "incus storage list --format=csv")
        if pools.returncode != 0 or pools.stdout.strip():
            pytest.skip(
                "test container's Incus is not in the expected fresh state "
                f"(storage list: rc={pools.returncode!r} out={pools.stdout!r})"
            )

        # Push the real install.sh and the harness alongside it.
        push = subprocess.run(
            ["incus", "file", "push", INSTALL_SH, f"{name}/root/install.sh"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert push.returncode == 0, f"failed to push install.sh: {push.stderr}"

        with tempfile.NamedTemporaryFile("w", suffix=".sh", delete=False) as fh:
            fh.write(HARNESS_SH)
            harness_path = fh.name
        try:
            push = subprocess.run(
                ["incus", "file", "push", harness_path, f"{name}/root/harness.sh"],
                capture_output=True,
                text=True,
                timeout=30,
            )
            assert push.returncode == 0, f"failed to push harness.sh: {push.stderr}"
        finally:
            os.unlink(harness_path)

        yield name
    finally:
        subprocess.run(["incus", "delete", name, "--force"], capture_output=True, check=False)


def _run_harness(container):
    result = _exec(container, "bash /root/harness.sh", timeout=120)
    assert "===RC=" in result.stdout, (
        f"harness did not complete.\nstdout: {result.stdout}\nstderr: {result.stderr}"
    )
    return result.stdout


def test_ensure_incus_initialized_against_real_daemon(incus_init_container):
    container = incus_init_container

    # --- Premise checks against REAL incus output (the #703 root cause) --------
    # A fresh daemon's network list is non-empty (host interfaces) but has no
    # MANAGED=YES row. This is exactly the state that fooled the old
    # `[ -n "$networks" ]` check.
    nets = _exec(container, "incus network list --format=csv")
    assert nets.returncode == 0, f"network list failed: {nets.stderr}"
    assert nets.stdout.strip() != "", (
        "fresh Incus network list was empty; expected unmanaged host interfaces. "
        "Premise of #703 no longer holds on this image."
    )
    managed_rows = [ln for ln in nets.stdout.splitlines() if ln.split(",")[2:3] == ["YES"]]
    assert managed_rows == [], f"fresh daemon unexpectedly has a MANAGED network: {managed_rows!r}"

    # --- Phase A: fresh daemon -> must DECIDE to init --------------------------
    out = _run_harness(container)
    assert "has not been initialized" in out, (
        f"on a fresh daemon the installer should report it is not initialized.\n{out}"
    )
    assert "incus admin init --auto" in _sudolog(out), (
        f"on a fresh daemon the installer must reach for `incus admin init --auto`.\n{out}"
    )

    # --- Phase B: a real storage pool exists -> must DECIDE to SKIP ------------
    # Reproduces an Incus initialized with a custom/existing network and no
    # managed bridge: zero MANAGED networks, but a pool. The mitigation on top of
    # the #703 fix must treat the pool as proof of initialization. Uses a real
    # `dir` pool (needs no special privileges under nesting).
    created = _exec(container, "incus storage create coitest dir")
    assert created.returncode == 0, f"could not create test storage pool: {created.stderr}"
    try:
        # Confirm the real pool CSV is what the shell check keys on.
        pools = _exec(container, "incus storage list --format=csv")
        assert "coitest" in pools.stdout, f"pool not listed: {pools.stdout!r}"

        out = _run_harness(container)
        assert "has not been initialized" not in out, (
            f"with a storage pool present the installer must NOT re-init.\n{out}"
        )
        assert "incus admin init --auto" not in _sudolog(out), (
            f"with a storage pool present the installer must not call admin init.\n{out}"
        )
    finally:
        _exec(container, "incus storage delete coitest", check=False)

    # --- Phase C: a real MANAGED network exists -> must DECIDE to SKIP ---------
    # Best-effort: creating a managed bridge can require privileges some nested
    # environments withhold. When it works, it validates that real Incus renders
    # the MANAGED column as the literal `YES` the shell greps for. When it does
    # not, the managed-network path is still covered by the Go stub tests, so we
    # skip only this sub-assertion.
    created = _exec(container, "incus network create coibr0")
    if created.returncode != 0:
        pytest.skip(
            "could not create a managed network in this nested environment; "
            f"MANAGED-network path left to the Go stub tests. ({created.stderr.strip()})"
        )
    try:
        nets = _exec(container, "incus network list --format=csv")
        managed = [ln for ln in nets.stdout.splitlines() if ln.startswith("coibr0,")]
        assert managed and managed[0].split(",")[2] == "YES", (
            f"expected coibr0 to render MANAGED=YES in real CSV, got: {managed!r}"
        )

        out = _run_harness(container)
        assert "has not been initialized" not in out, (
            f"with a MANAGED network present the installer must NOT re-init.\n{out}"
        )
        assert "incus admin init --auto" not in _sudolog(out), (
            f"with a MANAGED network present the installer must not call admin init.\n{out}"
        )
    finally:
        _exec(container, "incus network delete coibr0", check=False)
