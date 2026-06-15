"""
Test that network setup purges stale per-IP nft rules before applying policy.

nft rules in the `ip coi forward` chain are keyed by container IP (comment
"coi-<IP>"). Incus DHCP recycles leases, so a new container can reuse a prior
container's IP. If the prior container's rules were orphaned (unclean teardown),
and the chain is evaluated first-match-wins, an inherited blanket ACCEPT would
let a restricted successor bypass its filter.

This reproduces the scenario deterministically with a persistent container:
1. Start a restricted persistent container; note its IP.
2. Stop it (kept for reuse) and inject a uniquely fingerprinted ACCEPT rule for
   that IP — a TEST-NET-3 destination a real restricted setup never produces,
   standing in for an orphaned rule from a prior container that held the IP.
3. Restart the persistent container (same MAC -> same IP), which re-runs network
   setup. While it is running (before any teardown), assert the fingerprint rule
   is gone — i.e. setup purged coi-<IP> before applying the restricted policy.

Inspecting while the container runs is what makes this meaningful: checking after
teardown could not distinguish "purge worked" from "teardown cleaned up", since
both remove coi-<IP> rules.
"""

import json
import subprocess
import time
from pathlib import Path

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


def incus_available():
    try:
        subprocess.run(["incus", "version"], capture_output=True, timeout=10, check=True)
        return True
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired, FileNotFoundError):
        return False


def get_container_ip(name):
    result = subprocess.run(
        ["incus", "list", name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=15,
    )
    if result.returncode != 0 or not result.stdout.strip():
        return None
    info = json.loads(result.stdout)
    if not info:
        return None
    addrs = info[0].get("state", {}).get("network", {}).get("eth0", {}).get("addresses", [])
    for addr in addrs:
        if addr.get("family") == "inet":
            return addr["address"]
    return None


def container_running(name):
    result = subprocess.run(
        ["incus", "list", name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=15,
    )
    if result.returncode != 0 or not result.stdout.strip():
        return False
    info = json.loads(result.stdout)
    return bool(info) and info[0].get("status") == "Running"


def coi_forward_chain():
    result = subprocess.run(
        ["sudo", "-n", "nft", "list", "chain", "ip", "coi", "forward"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    return result.stdout if result.returncode == 0 else ""


def wait_for(predicate, timeout=60, interval=1.0):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if predicate():
            return True
        time.sleep(interval)
    return predicate()


def start_background_shell(coi_binary, workspace_dir):
    """Launch a persistent restricted container detached; return when setup is done."""
    return subprocess.run(
        [
            coi_binary,
            "shell",
            "--persistent",
            "--background",
            "--workspace",
            workspace_dir,
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=120,
        env={**_base_env()},
    )


def _base_env():
    import os

    env = dict(os.environ)
    env["COI_USE_DUMMY"] = "1"
    return env


def test_setup_purges_stale_rules_for_reused_ip(coi_binary, workspace_dir, cleanup_containers):
    if not nft_available():
        pytest.skip("nft not available")
    if not incus_available():
        pytest.skip("incus not available")

    container_name = calculate_container_name(workspace_dir, 1)

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("""
[network]
mode = "restricted"
""")

    try:
        # === Phase 1: start restricted persistent container, capture its IP ===
        result = start_background_shell(coi_binary, workspace_dir)
        assert result.returncode == 0, f"first shell should start. stderr: {result.stderr}"

        assert wait_for(lambda: get_container_ip(container_name) is not None, timeout=60), (
            "container should come up and acquire an IP"
        )
        container_ip = get_container_ip(container_name)

        # Restricted setup must have installed rules for this IP.
        assert wait_for(lambda: container_ip in coi_forward_chain(), timeout=30), (
            f"restricted rules for {container_ip} should be present after first setup"
        )

        # === Phase 2: stop (keep for reuse) and inject an orphan rule ===
        # Force-stop: a graceful stop can hang on the backgrounded tmux/tool
        # session. We only need the container stopped-and-kept for reuse.
        subprocess.run(
            ["incus", "stop", container_name, "--force"], capture_output=True, timeout=90
        )
        assert wait_for(lambda: not container_running(container_name), timeout=60), (
            "container should stop"
        )

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
        assert FINGERPRINT in coi_forward_chain(), (
            "precondition: injected stale rule must be present"
        )

        # === Phase 3: restart (same IP) -> setup re-runs -> purge ===
        result2 = start_background_shell(coi_binary, workspace_dir)
        assert result2.returncode == 0, f"second shell should start. stderr: {result2.stderr}"

        assert wait_for(lambda: container_running(container_name), timeout=60), (
            "persistent container should be restarted"
        )
        # Same persistent container -> same DHCP lease -> same IP.
        reused_ip = get_container_ip(container_name)
        assert reused_ip == container_ip, (
            f"persistent restart should reuse the same IP (was {container_ip}, got {reused_ip}); "
            f"test precondition for exercising the purge"
        )

        # While the restarted container is running (no teardown yet), the stale
        # rule must be gone — proving setup purged coi-<IP> before re-applying.
        assert wait_for(lambda: reused_ip in coi_forward_chain(), timeout=30), (
            "restricted policy should be re-applied for the reused IP"
        )
        assert FINGERPRINT not in coi_forward_chain(), (
            f"stale coi-{container_ip} rule ({FINGERPRINT}) survived restart — "
            f"DHCP-reuse inheritance was not purged by setup"
        )
    finally:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=60,
        )
