"""
Regression test for issue #548 — systemd-networkd-wait-online must not hang
when coi disables IPv6 in the container.

In restricted/allowlist mode coi disables IPv6 (DisableIPv6AtBoot pre-start +
DisableIPv6ForContainer post-start). With IPv6 kernel-disabled, systemd-networkd
otherwise keeps failing to add the IPv6 link-local address, so eth0 never leaves
the "configuring" setup state and systemd-networkd-wait-online — hence
network-online.target and everything ordered after it (docker.service, and the
reporter's ollama.service) — hangs indefinitely.

The fix pre-seeds an IPv4-only networkd config (05-coi-ipv4-only.network) so the
link reaches "configured" and wait-online completes. This test drives `coi run`
(the session-setup path, not `coi container launch`) in restricted mode on the
systemd coi-default image and asserts the boot transaction settles with
wait-online no longer queued. It runs a few times because the pre-fix hang was
an intermittent race between the IPv6 disable and networkd's boot config.

Lives in the `network` CI group (`tests/network`), which stands up the
restricted-mode nft/firewall environment.
"""

import os
import subprocess

import pytest

# Runs inside the coi container.
_BOOT_INSPECT = r"""
set +e
sleep 8
echo "SYSTEM_STATE=$(systemctl is-system-running 2>/dev/null)"
echo "=== list-jobs ==="
systemctl list-jobs --no-legend 2>/dev/null
echo "=== networkctl eth0 ==="
networkctl status eth0 --no-pager 2>/dev/null | sed -n '1,10p'
"""


def _set_restricted(workspace_dir):
    coi_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(coi_dir, exist_ok=True)
    with open(os.path.join(coi_dir, "config.toml"), "a") as f:
        f.write('\n[network]\nmode = "restricted"\n')


@pytest.mark.parametrize("attempt", range(1, 4))
def test_wait_online_not_stuck_when_ipv6_disabled(
    coi_binary, cleanup_containers, workspace_dir, attempt
):
    _set_restricted(workspace_dir)

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "bash", "-lc", _BOOT_INSPECT],
        capture_output=True,
        text=True,
        timeout=150,
    )
    out = result.stdout + result.stderr
    assert result.returncode == 0, f"coi run failed (attempt {attempt}):\n{out}"

    boot_jobs = out.split("=== list-jobs ===", 1)[-1].split("=== networkctl", 1)[0]
    settled = "SYSTEM_STATE=running" in out or "SYSTEM_STATE=degraded" in out
    wait_online_queued = "systemd-networkd-wait-online" in boot_jobs

    assert settled and not wait_online_queued, (
        f"#548 regression (attempt {attempt}): boot did not settle / "
        f"systemd-networkd-wait-online still queued with IPv6 disabled.\n{out}"
    )
