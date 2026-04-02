"""
Test for firewall rule cleanup bugs.

Tests that:
1. Bug #1: Open mode firewall rules are properly cleaned up after container deletion
2. Bug #2: Firewall cleanup happens BEFORE container deletion (correct order)
3. No firewall rule accumulation across multiple container lifecycles

These tests verify the fixes for the bugs documented in ERROR.md from coi-pond.
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


def get_firewall_rules():
    """Get all firewalld direct rules in the FORWARD chain."""
    try:
        result = subprocess.run(
            ["sudo", "-n", "firewall-cmd", "--direct", "--get-all-rules"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            return []

        rules = []
        for line in result.stdout.strip().split("\n"):
            line = line.strip()
            if line and "FORWARD" in line:
                rules.append(line)
        return rules
    except Exception:
        return []


def count_rules_for_ip(ip):
    """Count firewall rules that reference a specific IP."""
    rules = get_firewall_rules()
    return sum(1 for rule in rules if ip in rule)


def cleanup_rules_for_ip(ip):
    """Manually clean up any orphaned rules for an IP (for test hygiene)."""
    rules = get_firewall_rules()
    for rule in rules:
        if ip in rule:
            parts = rule.split()
            if len(parts) >= 4:
                args = ["sudo", "-n", "firewall-cmd", "--direct", "--remove-rule", *parts]
                subprocess.run(args, capture_output=True, timeout=10, check=False)


def firewalld_available():
    """Check if firewalld is available and running."""
    result = subprocess.run(
        ["sudo", "-n", "firewall-cmd", "--state"],
        capture_output=True,
        timeout=10,
    )
    return result.returncode == 0


def test_open_mode_firewall_cleanup(coi_binary, workspace_dir, cleanup_containers):
    """
    Test Bug #1: Open mode firewall rules should be cleaned up.

    Previously, open mode containers created firewall rules via EnsureOpenModeRules()
    but Teardown() returned early for open mode without removing those rules.

    This test verifies that:
    1. Open mode creates firewall rules (for FORWARD chain policy DROP)
    2. When container exits, those rules are properly cleaned up
    3. No orphaned rules remain
    """
    import pytest

    if not firewalld_available():
        pytest.skip("firewalld not available")

    # Count rules before
    rules_before = len(get_firewall_rules())

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
        # Open mode creates at least 1 ACCEPT rule
        assert rules_for_container >= 1, (
            f"Open mode should create ACCEPT rule for IP {container_ip}, "
            f"but found {rules_for_container} rules"
        )

    # Use coi kill which triggers proper cleanup path
    subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        timeout=30,
        check=False,
    )

    # Wait for cleanup
    time.sleep(2)

    # Count rules after cleanup
    rules_after_cleanup = len(get_firewall_rules())

    # If we have the container IP, check for orphaned rules
    if container_ip:
        orphaned_rules = count_rules_for_ip(container_ip)
        if orphaned_rules > 0:
            # Clean up for test hygiene
            cleanup_rules_for_ip(container_ip)
            pytest.fail(
                f"Bug #1: Found {orphaned_rules} orphaned firewall rules for IP {container_ip} "
                f"after cleanup. Open mode rules were not properly removed."
            )

    # General check: rule count should not have grown significantly
    # Allow +1 for base conntrack rule that might be added
    assert rules_after_cleanup <= rules_before + 1, (
        f"Firewall rule leak detected: had {rules_before} rules before, "
        f"{rules_after_cleanup} after cleanup"
    )


def test_restricted_mode_firewall_cleanup(coi_binary, workspace_dir, cleanup_containers):
    """
    Test Bug #2: Firewall cleanup must happen BEFORE container deletion.

    Previously, cleanup.go deleted the container first, then tried to clean up
    firewall rules. But RemoveRules() needs the container IP, which is no longer
    available after deletion.

    This test verifies that:
    1. Restricted mode creates firewall rules
    2. When container exits, rules are cleaned up
    3. The cleanup order is correct (network teardown before container delete)
    """
    import pytest

    if not firewalld_available():
        pytest.skip("firewalld not available")

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
            f"Restricted mode should create firewall rules for IP {container_ip}"
        )

    # Kill container (which should trigger proper cleanup)
    subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        timeout=30,
        check=False,
    )

    # Wait for cleanup
    time.sleep(2)

    # Check for orphaned rules
    if container_ip:
        orphaned_rules = count_rules_for_ip(container_ip)
        if orphaned_rules > 0:
            cleanup_rules_for_ip(container_ip)
            pytest.fail(
                f"Bug #2: Found {orphaned_rules} orphaned firewall rules for IP {container_ip}. "
                f"This indicates cleanup happened after container deletion."
            )


def test_no_firewall_rule_accumulation(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that firewall rules don't accumulate across container lifecycles.

    This is the combined effect of Bug #1 and Bug #2. When running many containers
    (like in integration tests), orphaned rules accumulate and degrade system performance.

    This test launches 3 containers in sequence and verifies no rule accumulation.
    """
    import pytest

    if not firewalld_available():
        pytest.skip("firewalld not available")

    # Count rules at start
    rules_at_start = len(get_firewall_rules())

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

    # Check final rule count
    rules_at_end = len(get_firewall_rules())

    # Check for any orphaned rules from our containers
    total_orphaned = 0
    for ip in collected_ips:
        orphaned = count_rules_for_ip(ip)
        if orphaned > 0:
            total_orphaned += orphaned
            cleanup_rules_for_ip(ip)

    # Should have no significant rule accumulation
    # Allow +1 for base conntrack rule
    assert rules_at_end <= rules_at_start + 1, (
        f"Rule accumulation detected: started with {rules_at_start}, "
        f"ended with {rules_at_end} after 3 container lifecycles"
    )

    assert total_orphaned == 0, (
        f"Found {total_orphaned} total orphaned rules across {len(collected_ips)} containers"
    )


def test_kill_cleans_up_restricted_rules(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that 'coi kill' on a running container properly cleans up firewall rules.

    This verifies the fix where cleanup order was corrected:
    1. Get container IP while container is still running
    2. Clean up firewall rules
    3. Then delete container

    Note: Testing cleanup after 'sudo shutdown 0' is not possible with --background
    mode since there's no cleanup loop. The interactive session cleanup is tested
    via the Go integration tests.
    """
    import pytest

    if not firewalld_available():
        pytest.skip("firewalld not available")

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

    # Verify rules exist
    rules_before_kill = count_rules_for_ip(container_ip)
    assert rules_before_kill > 0, f"Restricted mode should have created rules for {container_ip}"

    # Kill the running container - this should clean up firewall rules first
    subprocess.run(
        [coi_binary, "kill", container_name],
        capture_output=True,
        timeout=30,
        check=False,
    )

    # Wait for cleanup
    time.sleep(2)

    # Check if rules were cleaned up
    rules_after_kill = count_rules_for_ip(container_ip)

    if rules_after_kill > 0:
        # Clean up for test hygiene
        cleanup_rules_for_ip(container_ip)
        pytest.fail(
            f"Cleanup order bug: {rules_after_kill} rules still exist for {container_ip}. "
            f"This indicates firewall cleanup failed."
        )
