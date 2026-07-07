"""
Reproduction probe for issue #548 — systemd-networkd-wait-online hangs in coi
containers despite a routable interface, stalling anything with a
network-online.target dependency (docker.service in the base image included).

The reporter isolated the hang to coi's SESSION-SETUP path (`coi run` / `coi
shell`), which — unlike a raw `incus launch` — applies pre-start / at-start
network hardening to prevent an unsupervised boot traffic window:

  - EnableNICSecurity: security.ipv4_filtering / mac_filtering / port_isolation
  - DisableIPv6AtBoot (restricted/allowlist): net.ipv6.conf.*.disable_ipv6=1
  - ApplyBootBlockRule: a host-side iifname<veth> DROP in the ip coi forward
    chain, held from start until SetupForContainer installs isolation.

This spec exercises that exact path in **restricted mode** (full hardening) on
the systemd coi-default image and asks two things inside the container:

  1. faithful: after a boot settle, has the boot transaction finished, or is
     systemd-networkd-wait-online still queued? (reporter step 3)
  2. deterministic: does `systemctl start systemd-networkd-wait-online` complete,
     or block? (reporter: "running systemctl start by hand just blocks")

It is a REPRODUCTION probe: it ASSERTS a clean boot, so a red here IS the
repro. Looped a few times because the reporter describes it as intermittent.

Run via the `network` CI group (`tests/network`), which already stands up the
restricted-mode nft/firewall environment.
"""

import os
import subprocess

import pytest

# Runs inside the coi container. Kept POSIX-sh compatible.
_BOOT_INSPECT = r"""
set +e
sleep 8
echo "SYSTEM_STATE=$(systemctl is-system-running 2>/dev/null)"
echo "=== list-jobs (boot transaction) ==="
systemctl list-jobs --no-legend 2>/dev/null
# Reporter's deterministic symptom: a manual start blocks forever. 45s cap;
# rc=124 from `timeout` means it never completed => the hang is reproduced.
timeout 45 systemctl start systemd-networkd-wait-online.service
echo "WAIT_ONLINE_RC=$?"
echo "=== networkctl eth0 ==="
networkctl status eth0 --no-pager 2>/dev/null | sed -n '1,10p'
echo "=== journalctl -u systemd-networkd (tail) ==="
journalctl -u systemd-networkd --no-pager 2>/dev/null | tail -15
"""


def _restricted_workspace(workspace_dir):
    """Force [network] mode = restricted so the full pre-start hardening +
    fail-closed boot block are applied (project-scope config, honored)."""
    coi_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(coi_dir, exist_ok=True)
    with open(os.path.join(coi_dir, "config.toml"), "a") as f:
        f.write('\n[network]\nmode = "restricted"\n')


@pytest.mark.parametrize("attempt", range(1, 5))
def test_wait_online_does_not_hang_in_restricted_mode(
    coi_binary, cleanup_containers, workspace_dir, attempt
):
    _restricted_workspace(workspace_dir)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "bash",
            "-lc",
            _BOOT_INSPECT,
        ],
        capture_output=True,
        text=True,
        timeout=150,
    )
    out = result.stdout + result.stderr
    assert result.returncode == 0, f"coi run itself failed (attempt {attempt}):\n{out}"

    # (2) deterministic signal: manual wait-online start timed out => hung.
    hung_on_manual_start = "WAIT_ONLINE_RC=124" in out

    # (1) faithful signal: boot never settled AND wait-online still queued.
    boot_section = out.split("=== list-jobs (boot transaction) ===", 1)[-1]
    boot_jobs = boot_section.split("WAIT_ONLINE_RC", 1)[0]
    settled = "SYSTEM_STATE=running" in out or "SYSTEM_STATE=degraded" in out
    queued_at_boot = "systemd-networkd-wait-online" in boot_jobs
    hung_at_boot = queued_at_boot and not settled

    assert not (hung_on_manual_start or hung_at_boot), (
        f"issue #548 reproduced on attempt {attempt}: "
        f"wait-online hung (manual_start_timeout={hung_on_manual_start}, "
        f"boot_transaction_stuck={hung_at_boot}).\n{out}"
    )
