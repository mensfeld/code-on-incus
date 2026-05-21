"""
Test that the container's hostname is present in /etc/hosts.

Without the coi-fix-hostname systemd service, sudo inside a container prints
"unable to resolve host <name>" because Incus sets a new hostname at boot that
wasn't known when the image was built.  This test catches regressions by
verifying that the container's hostname appears in /etc/hosts at runtime.
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
        "The coi-fix-hostname service may not have run correctly."
    )
