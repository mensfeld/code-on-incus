"""
Test that the container's hostname is present in /etc/hosts.

Incus assigns the hostname at boot; /etc/hosts is baked into the image at build
time with a different name.  Without /etc/rc.local adding the runtime hostname,
sudo prints "unable to resolve host <name>" on every invocation.

This test catches regressions. The fix lives in build.sh (the
coi-fix-hostname.service oneshot), and the CI image cache key includes
internal/image/**, so the built image carries the fix — the test runs for real.
"""

import subprocess


def test_hostname_in_hosts(coi_binary, cleanup_containers, workspace_dir):
    """
    Verify that the container hostname resolves via /etc/hosts.

    Flow:
    1. Run `hostname` to get the container name.
    2. Run `grep <hostname> /etc/hosts` to confirm the entry exists.
    """
    # Step 1: get the hostname assigned by Incus
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "hostname"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"coi run hostname failed: {result.stderr}"

    hostname = (result.stdout + result.stderr).strip().splitlines()[-1].strip()
    assert hostname, "Could not determine container hostname"

    # Step 2: confirm it appears in /etc/hosts
    result2 = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "grep", hostname, "/etc/hosts"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    combined = result2.stdout + result2.stderr
    assert result2.returncode == 0 and hostname in combined, (
        f"Hostname '{hostname}' not found in /etc/hosts.\n"
        f"grep output: {combined}\n"
        "The /etc/rc.local hostname fix may not have run correctly."
    )
