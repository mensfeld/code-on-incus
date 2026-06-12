"""
Test that 'coi run' with network isolation properly cleans up nft rules
when interrupted with SIGINT.

Bug: applyNetworkRunPhase used `func(_ context.Context)` and called
applyNetworkIsolation(context.Background(), ...). After the fix, the phase
receives and forwards the caller's context so cancellation is respected
during setup, while the teardown always uses a fresh context.Background()
so firewall rules are removed even after SIGINT.
"""

import pathlib
import signal
import subprocess
import time


def nft_available():
    """Check if nft is available with sudo access."""
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


def count_nft_rules_for_ip(ip):
    """Count rules in the ip coi forward chain that reference a given IP."""
    try:
        result = subprocess.run(
            ["sudo", "-n", "nft", "-a", "list", "chain", "ip", "coi", "forward"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            return 0
        return sum(1 for line in result.stdout.splitlines() if ip in line)
    except Exception:
        return 0


def get_container_ip(coi_binary, workspace_dir):
    """Start a container, fetch its IP, then stop it — used for setup only."""
    result = subprocess.run(
        [coi_binary, "shell", "--workspace", str(workspace_dir), "--background"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    if result.returncode != 0:
        return None, None

    container_name = None
    for line in result.stderr.splitlines():
        if "Container name:" in line:
            container_name = line.split("Container name:")[-1].strip()
            break

    if not container_name:
        return None, None

    time.sleep(3)

    ip_result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "hostname", "-I"],
        capture_output=True,
        text=True,
        timeout=20,
    )
    ip = None
    if ip_result.returncode == 0:
        parts = ip_result.stdout.strip().split()
        if parts:
            ip = parts[0]

    subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        timeout=30,
        check=False,
    )
    return container_name, ip


def test_run_restricted_nft_cleanup_on_sigint(coi_binary, workspace_dir, cleanup_containers):
    """
    coi run with restricted mode should remove nft rules even when SIGINT is
    sent while the command is running.

    Verifies that the context is properly propagated through the network phase
    so that the teardown closure (which uses context.Background() intentionally)
    always fires and removes the rules.
    """
    import pytest

    if not nft_available():
        pytest.skip("nft not available")

    config_dir = pathlib.Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text('[network]\nmode = "restricted"\n')

    # Start a long-running `coi run` in the background so we can interrupt it.
    proc = subprocess.Popen(
        [
            coi_binary,
            "run",
            "--workspace",
            str(workspace_dir),
            "sleep 60",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )

    # Give the container and nft rules time to be created.
    time.sleep(10)

    # Grab the container's IP from the nft chain while rules still exist.
    nft_result = subprocess.run(
        ["sudo", "-n", "nft", "-a", "list", "chain", "ip", "coi", "forward"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    container_ip = None
    for line in nft_result.stdout.splitlines():
        # Rules are tagged with the container IP as source/dest
        for part in line.split():
            if part.startswith("10.") and "." in part:
                container_ip = part
                break
        if container_ip:
            break

    if not container_ip:
        proc.send_signal(signal.SIGINT)
        proc.wait(timeout=30)
        pytest.skip(
            "Could not find container IP in nft chain — nft rules may not have been created"
        )

    rules_before = count_nft_rules_for_ip(container_ip)
    assert rules_before > 0, f"Expected nft rules for {container_ip} before SIGINT, found none"

    # Send SIGINT to simulate Ctrl+C.
    proc.send_signal(signal.SIGINT)

    try:
        proc.wait(timeout=30)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()
        pytest.fail("coi run did not exit within 30s after SIGINT")

    # Retry for up to 10s: nft rule deletion may lag slightly behind process exit
    # on loaded CI runners (same pattern used in test_no_nft_rule_accumulation).
    deadline = time.time() + 10
    rules_after = count_nft_rules_for_ip(container_ip)
    while rules_after > 0 and time.time() < deadline:
        time.sleep(1)
        rules_after = count_nft_rules_for_ip(container_ip)

    assert rules_after == 0, (
        f"Found {rules_after} orphaned nft rules for {container_ip} after SIGINT. "
        "Network teardown did not complete despite context fix."
    )
