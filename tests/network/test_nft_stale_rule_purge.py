"""
Test that network setup purges stale per-IP nft rules before applying policy.

nft rules in the `ip coi forward` chain are keyed by container IP (comment
"coi-<IP>"), and the chain is evaluated first-match-wins. Incus DHCP recycles
leases, so a new container can reuse a prior container's IP. If the prior
container's rules were orphaned (unclean teardown), a leftover blanket ACCEPT
sitting ahead of a restricted successor's rules would let it bypass its filter.

Reproduced deterministically with a persistent container pinned to a fixed slot:
1. `coi run --persistent --slot 1 -- sleep` (background); once the restricted
   rules appear in the chain, capture the container IP and SIGINT the run. Its
   teardown removes the rules; persistent mode keeps the container (stopped).
2. Inject a uniquely fingerprinted ACCEPT rule for that IP (a TEST-NET-3
   destination a real restricted setup never produces) — an orphan stand-in.
3. `coi run --persistent --slot 1 -- sleep` again. Same persistent container is
   restarted (same MAC -> same IP) and network setup re-runs. While the run is
   live (before its teardown), assert the fingerprint rule is gone — i.e. setup
   purged coi-<IP> before re-applying the restricted policy.

Inspecting while the run is live is what makes this meaningful: checking after
teardown could not distinguish "purge worked" from "teardown cleaned up", since
both remove coi-<IP> rules.
"""

import pathlib
import signal
import subprocess
import time

import pytest

from support.helpers import calculate_container_name

# RFC 5737 TEST-NET-3 — never a real resolved/allowlisted address, so a clean
# restricted setup never produces a rule referencing it.
FINGERPRINT = "203.0.113.7"


def nft_available():
    try:
        subprocess.run(
            ["sudo", "-n", "nft", "list", "ruleset"],
            capture_output=True,
            timeout=10,
            check=True,
        )
        return True
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired, FileNotFoundError):
        return False


def chain_text():
    result = subprocess.run(
        ["sudo", "-n", "nft", "list", "chain", "ip", "coi", "forward"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    return result.stdout if result.returncode == 0 else ""


def container_ips_in_chain():
    """Container IPs from comment "coi-<IP>" tags (ignores coi-base/coi-boot)."""
    ips = set()
    prefix = 'comment "coi-'
    for line in chain_text().splitlines():
        if prefix not in line:
            continue
        start = line.index(prefix) + len(prefix)
        end = line.index('"', start)
        ip = line[start:end]
        if "." in ip:  # skip "base"; boot rules are keyed by container name
            ips.add(ip)
    return ips


def count_rules_for_ip(ip):
    comment = f'comment "coi-{ip}"'
    return sum(1 for line in chain_text().splitlines() if comment in line)


def start_run(coi_binary, workspace_dir):
    """Start a long-lived `coi run` on slot 1 so the container stays up to inspect."""
    return subprocess.Popen(
        [
            coi_binary,
            "run",
            "--persistent",
            "--slot",
            "1",
            "--workspace",
            str(workspace_dir),
            "sleep",
            "180",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def stop_run(proc):
    proc.send_signal(signal.SIGINT)
    try:
        proc.wait(timeout=30)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()


def test_setup_purges_stale_rules_for_reused_ip(coi_binary, workspace_dir, cleanup_containers):
    if not nft_available():
        pytest.skip("nft not available")

    container_name = calculate_container_name(workspace_dir, 1)
    config_dir = pathlib.Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text('[network]\nmode = "restricted"\n')

    pre_ips = container_ips_in_chain()

    try:
        # === Phase 1: bring the container up, capture its IP ===
        proc1 = start_run(coi_binary, workspace_dir)
        container_ip = None
        deadline = time.time() + 90
        while time.time() < deadline:
            new_ips = container_ips_in_chain() - pre_ips
            if new_ips:
                container_ip = next(iter(new_ips))
                break
            if proc1.poll() is not None:
                break
            time.sleep(1)

        if not container_ip:
            stop_run(proc1)
            pytest.skip("container IP did not appear in nft chain within 90s")

        assert count_rules_for_ip(container_ip) > 0, (
            "restricted rules should exist after first setup"
        )

        # SIGINT: teardown removes the rules; persistent mode keeps the container.
        stop_run(proc1)

        # === Phase 2: inject an orphan rule for that IP ===
        inject = subprocess.run(
            [
                "sudo",
                "-n",
                "nft",
                "add",
                "rule",
                "ip",
                "coi",
                "forward",
                "ip",
                "saddr",
                container_ip,
                "ip",
                "daddr",
                f"{FINGERPRINT}/32",
                "accept",
                "comment",
                f"coi-{container_ip}",
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert inject.returncode == 0, (
            f"injecting stale rule should succeed. stderr: {inject.stderr}"
        )
        assert FINGERPRINT in chain_text(), "precondition: injected stale rule must be present"

        # === Phase 3: restart the same persistent container -> setup re-runs ===
        proc2 = start_run(coi_binary, workspace_dir)
        reapplied = False
        deadline = time.time() + 90
        while time.time() < deadline:
            # Same persistent container -> same IP -> restricted rules reappear
            # for container_ip once setup has run (purge + apply).
            if count_rules_for_ip(container_ip) > 0:
                reapplied = True
                break
            if proc2.poll() is not None:
                break
            time.sleep(1)

        try:
            assert reapplied, (
                f"restricted policy should be re-applied for reused IP {container_ip} on restart"
            )
            # While the run is live (no teardown yet), the stale rule must be gone.
            current = chain_text()
            assert FINGERPRINT not in current, (
                f"stale coi-{container_ip} rule ({FINGERPRINT}) survived restart — "
                f"DHCP-reuse inheritance was not purged by setup.\nchain:\n{current}"
            )
        finally:
            stop_run(proc2)
    finally:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=60,
        )
