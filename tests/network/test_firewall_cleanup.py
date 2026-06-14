"""
Test for nft rule cleanup bugs.

Tests that:
1. Bug #1: Open mode nft rules are properly cleaned up after container deletion
2. Bug #2: nft cleanup happens BEFORE container deletion (correct order)
3. No nft rule accumulation across multiple container lifecycles

These tests verify the fixes for the bugs documented in ERROR.md from coi-pond.
nft rules live in the `ip coi forward` chain and are identified by handle.
`ListCOIFilterRuleIPs()` returns a map of containerIP → []handles from
`nft -a list chain ip coi forward`.
`DeleteCOIFilterRulesForIP(ip)` deletes rules by handle.
"""

import json
import subprocess
import time
from pathlib import Path


def get_container_ip(coi_binary, container_name):
    """Get container IP using coi container exec with --capture."""
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--capture", "--", "hostname", "-I"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    if result.returncode != 0:
        return None

    try:
        # --capture outputs JSON with stdout/stderr fields
        data = json.loads(result.stdout)
        stdout = data.get("stdout", "").strip()
        if stdout:
            # hostname -I returns space-separated IPs, get the container IP (usually 10.x.x.x)
            ips = stdout.split()
            for ip in ips:
                if ip.startswith("10."):
                    return ip
            # Fallback to first IP
            return ips[0] if ips else None
    except json.JSONDecodeError:
        # Fallback: try parsing stdout directly
        if result.stdout.strip():
            return result.stdout.strip().split()[0]

    return None


def get_nft_rules_for_ip(ip):
    """Get nft rules from the ip coi forward chain that reference a specific IP."""
    try:
        result = subprocess.run(
            ["sudo", "-n", "nft", "-a", "list", "chain", "ip", "coi", "forward"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            return []

        rules = []
        for line in result.stdout.strip().split("\n"):
            line = line.strip()
            if line and ip in line:
                rules.append(line)
        return rules
    except Exception:
        return []


def count_rules_for_ip(ip):
    """Count nft rules in the ip coi forward chain that reference a specific IP."""
    return len(get_nft_rules_for_ip(ip))


def wait_for_no_rules_for_ip(ip, timeout=15):
    """Poll until no nft rules reference ip (or timeout); return the final count.

    nft cleanup after kill/teardown is asynchronous and can lag on loaded CI
    runners, so poll rather than asserting once after a fixed sleep.
    """
    deadline = time.time() + timeout
    count = count_rules_for_ip(ip)
    while count > 0 and time.time() < deadline:
        time.sleep(1)
        count = count_rules_for_ip(ip)
    return count


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


def test_open_mode_nft_cleanup(coi_binary, workspace_dir, cleanup_containers):
    """
    Test Bug #1: Open mode nft rules should be cleaned up.

    Previously, open mode containers created nft rules via EnsureOpenModeRules()
    but Teardown() returned early for open mode without removing those rules.

    This test verifies that:
    1. Open mode creates nft rules in the ip coi forward chain (for FORWARD chain policy DROP)
    2. When container exits, those rules are properly cleaned up
    3. No orphaned rules remain
    """
    import pytest

    if not nft_available():
        pytest.skip("nft not available")

    # Write network config to workspace .coi/config.toml
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("""
[network]
mode = "open"
""")

    # Start shell in background with open network mode
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--background",
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start. stderr: {result.stderr}"

    # Extract container name
    container_name = None
    for line in result.stderr.split("\n"):
        if "Container name:" in line:
            container_name = line.split("Container name:")[-1].strip()
            break

    assert container_name, f"Should find container name. stderr: {result.stderr}"

    # Wait for container to start and get IP
    time.sleep(5)

    # Get container IP for verification
    container_ip = get_container_ip(coi_binary, container_name)

    # Verify rules were created for open mode
    if container_ip:
        rules_for_container = count_rules_for_ip(container_ip)
        # Open mode creates at least 1 ACCEPT rule in the ip coi forward chain
        assert rules_for_container >= 1, (
            f"Open mode should create nft rule for IP {container_ip} in ip coi forward, "
            f"but found {rules_for_container} rules"
        )

    # Use coi kill which triggers proper cleanup path
    subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        timeout=30,
        check=False,
    )

    # Poll for asynchronous nft cleanup to complete.
    if container_ip:
        orphaned_rules = wait_for_no_rules_for_ip(container_ip)
        if orphaned_rules > 0:
            pytest.fail(
                f"Bug #1: Found {orphaned_rules} orphaned nft rules for IP {container_ip} "
                f"in ip coi forward after cleanup. Open mode rules were not properly removed."
            )


def test_restricted_mode_nft_cleanup(coi_binary, workspace_dir, cleanup_containers):
    """
    Test Bug #2: nft cleanup must happen BEFORE container deletion.

    Previously, cleanup.go deleted the container first, then tried to clean up
    nft rules. But DeleteCOIFilterRulesForIP() needs the container IP (or handles),
    which are no longer available after deletion.

    This test verifies that:
    1. Restricted mode creates nft rules in the ip coi forward chain
    2. When container exits, rules are cleaned up
    3. The cleanup order is correct (network teardown before container delete)
    """
    import pytest

    if not nft_available():
        pytest.skip("nft not available")

    # Write network config to workspace .coi/config.toml
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("""
[network]
mode = "restricted"
""")

    # Start shell with restricted network mode
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--background",
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start. stderr: {result.stderr}"

    # Extract container name
    container_name = None
    for line in result.stderr.split("\n"):
        if "Container name:" in line:
            container_name = line.split("Container name:")[-1].strip()
            break

    assert container_name, f"Should find container name. stderr: {result.stderr}"

    # Wait for container to start
    time.sleep(5)

    # Get container IP
    container_ip = get_container_ip(coi_binary, container_name)

    # Verify rules were created for restricted mode
    if container_ip:
        rules_for_container = count_rules_for_ip(container_ip)
        assert rules_for_container > 0, (
            f"Restricted mode should create nft rules for IP {container_ip} in ip coi forward"
        )

    # Kill container (which should trigger proper cleanup)
    subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        timeout=30,
        check=False,
    )

    # Poll for asynchronous nft cleanup to complete.
    if container_ip:
        orphaned_rules = wait_for_no_rules_for_ip(container_ip)
        if orphaned_rules > 0:
            pytest.fail(
                f"Bug #2: Found {orphaned_rules} orphaned nft rules for IP {container_ip} "
                f"in ip coi forward. This indicates cleanup happened after container deletion."
            )


def test_no_nft_rule_accumulation(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that nft rules don't accumulate across container lifecycles.

    This is the combined effect of Bug #1 and Bug #2. When running many containers
    (like in integration tests), orphaned rules accumulate and degrade system performance.

    This test launches 3 containers in sequence and verifies no rule accumulation.
    """
    import pytest

    if not nft_available():
        pytest.skip("nft not available")

    collected_ips = []

    for i in range(3):
        # Create unique workspace for each iteration to get different container
        iter_workspace = f"{workspace_dir}/iter{i}"
        subprocess.run(["mkdir", "-p", iter_workspace], check=True)

        # Write network config to workspace .coi/config.toml
        iter_config_dir = Path(iter_workspace) / ".coi"
        iter_config_dir.mkdir(exist_ok=True)
        (iter_config_dir / "config.toml").write_text("""
[network]
mode = "open"
""")

        # Start container with open mode
        result = subprocess.run(
            [
                coi_binary,
                "shell",
                "--workspace",
                iter_workspace,
                "--background",
            ],
            capture_output=True,
            text=True,
            timeout=60,
        )

        if result.returncode != 0:
            continue

        # Extract container name
        container_name = None
        for line in result.stderr.split("\n"):
            if "Container name:" in line:
                container_name = line.split("Container name:")[-1].strip()
                break

        if not container_name:
            continue

        # Wait for container
        time.sleep(3)

        # Get IP
        container_ip = get_container_ip(coi_binary, container_name)
        if container_ip:
            collected_ips.append(container_ip)

        # Kill container (triggers cleanup)
        subprocess.run(
            [coi_binary, "kill", container_name],
            capture_output=True,
            timeout=30,
            check=False,
        )

        time.sleep(1)

    # Check for any orphaned rules from our containers. On loaded CI runners
    # the nft rule deletion may lag the kill by a fraction of a second, so
    # retry for up to 20 seconds before failing (cleanup can lag on loaded CI).
    deadline = time.time() + 20
    total_orphaned = 0
    while True:
        total_orphaned = 0
        for ip in collected_ips:
            orphaned = count_rules_for_ip(ip)
            if orphaned > 0:
                total_orphaned += orphaned
        if total_orphaned == 0 or time.time() >= deadline:
            break
        time.sleep(1)

    assert total_orphaned == 0, (
        f"Found {total_orphaned} total orphaned nft rules in ip coi forward "
        f"across {len(collected_ips)} containers"
    )


def test_kill_cleans_up_restricted_rules(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that 'coi kill' on a running container properly cleans up nft rules.

    This verifies the fix where cleanup order was corrected:
    1. Get container IP while container is still running
    2. Clean up nft rules (delete by handle from ip coi forward)
    3. Then delete container

    Note: Testing cleanup after 'sudo shutdown 0' is not possible with --background
    mode since there's no cleanup loop. The interactive session cleanup is tested
    via the Go integration tests.
    """
    import pytest

    if not nft_available():
        pytest.skip("nft not available")

    # Write network config to workspace .coi/config.toml
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("""
[network]
mode = "restricted"
""")

    # Start shell with restricted mode (creates more rules to verify cleanup)
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--background",
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start. stderr: {result.stderr}"

    # Extract container name
    container_name = None
    for line in result.stderr.split("\n"):
        if "Container name:" in line:
            container_name = line.split("Container name:")[-1].strip()
            break

    assert container_name, "Should find container name"

    # Wait for container
    time.sleep(5)

    # Get container IP
    container_ip = get_container_ip(coi_binary, container_name)

    assert container_ip, "Should be able to get container IP"

    # Verify rules exist in ip coi forward
    rules_before_kill = count_rules_for_ip(container_ip)
    assert rules_before_kill > 0, (
        f"Restricted mode should have created nft rules for {container_ip} in ip coi forward"
    )

    # Kill the running container - this should clean up nft rules first
    subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        timeout=30,
        check=False,
    )

    # Poll for asynchronous nft cleanup to complete.
    rules_after_kill = wait_for_no_rules_for_ip(container_ip)

    if rules_after_kill > 0:
        pytest.fail(
            f"Cleanup order bug: {rules_after_kill} nft rules still exist for {container_ip} "
            f"in ip coi forward. This indicates nft cleanup failed."
        )
