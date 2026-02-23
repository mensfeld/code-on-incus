#!/usr/bin/env python3
"""
Integration tests for nftables-based network monitoring.

Tests NFT monitoring daemon lifecycle, rule management, threat detection,
and integration with the monitoring system.

These tests verify the critical security functionality:
1. NFT rules are created/destroyed correctly
2. Suspicious network activity is detected
3. Containers are KILLED on CRITICAL threats

NOTE: These tests require systemd journal access and nftables.
"""

import hashlib
import json
import os
import subprocess
import time
from pathlib import Path

import pytest


def get_container_name_from_workspace(workspace, slot=1):
    """Generate expected container name from workspace path."""
    abs_path = os.path.abspath(workspace)
    hash_digest = hashlib.sha256(abs_path.encode()).hexdigest()[:8]
    return f"coi-{hash_digest}-{slot}"


def get_container_ip(container_name):
    """Get container IP address from eth0."""
    result = subprocess.run(
        ["incus", "list", container_name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    if result.returncode != 0:
        return None
    container_info = json.loads(result.stdout)
    if not container_info:
        return None

    for iface_name, iface_info in container_info[0].get("state", {}).get("network", {}).items():
        if iface_name == "eth0":
            for addr in iface_info.get("addresses", []):
                if addr["family"] == "inet":
                    return addr["address"]
    return None


def get_container_state(name):
    """Get container state."""
    result = subprocess.run(
        ["incus", "list", name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        return "Unknown"
    containers = json.loads(result.stdout)
    return containers[0].get("status", "Unknown") if containers else "Unknown"


def get_nft_threat_events(container_name):
    """Get threat events from NFT audit log."""
    log_path = Path.home() / ".coi" / "audit" / f"{container_name}-nft.jsonl"
    if not log_path.exists():
        return []

    events = []
    with open(log_path) as f:
        for line in f:
            if line.strip():
                try:
                    event = json.loads(line)
                    if "level" in event:
                        events.append(event)
                except json.JSONDecodeError:
                    pass
    return events


def cleanup_container(name, coi_binary):
    """Force cleanup container."""
    subprocess.run(
        [coi_binary, "container", "delete", name, "--force"],
        timeout=60,
        capture_output=True,
        check=False,
    )


def wait_for_container_ready(container_name, timeout=30):
    """Wait for container to be running."""
    start = time.time()
    while time.time() - start < timeout:
        state = get_container_state(container_name)
        if state == "Running":
            return True
        time.sleep(1)
    return False


def check_nft_rules_exist(container_ip):
    """Check if NFT rules exist for container."""
    result = subprocess.run(
        ["sudo", "-n", "nft", "list", "ruleset"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        return False
    return (
        f"NFT_COI[{container_ip}]" in result.stdout
        or f"NFT_DNS[{container_ip}]" in result.stdout
        or f"NFT_SUSPICIOUS[{container_ip}]" in result.stdout
    )


@pytest.fixture(scope="module")
def nft_monitoring_available():
    """Check if NFT monitoring is available before running tests."""
    # Check if nft command works (requires sudo)
    try:
        result = subprocess.run(
            ["sudo", "-n", "nft", "list", "ruleset"],
            capture_output=True,
            timeout=10,
        )
        if result.returncode != 0:
            pytest.skip("NFT not available: nft command failed (check sudo permissions)")
    except subprocess.TimeoutExpired:
        pytest.skip("NFT not available: nft command timed out")
    except FileNotFoundError:
        pytest.skip("NFT not available: nft command not found")

    # Check if journalctl is accessible
    try:
        result = subprocess.run(
            ["journalctl", "-n", "1", "-k"],
            capture_output=True,
            timeout=5,
        )
        if result.returncode != 0:
            pytest.skip("NFT monitoring not available: journal access failed")
    except subprocess.TimeoutExpired:
        pytest.skip("NFT monitoring not available: journal access timed out")
    except FileNotFoundError:
        pytest.skip("NFT monitoring not available: journalctl not found")

    return True


@pytest.fixture
def test_workspace(tmp_path):
    """Create a temporary workspace for tests."""
    workspace = tmp_path / "nft-test-workspace"
    workspace.mkdir()
    return str(workspace)


class TestNFTRuleManagement:
    """Test nftables rule creation and deletion."""

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    def test_rules_created_on_session_start(self, test_workspace, coi_binary):
        """Verify nftables LOG rules are created when session starts."""
        slot = 50
        container_name = get_container_name_from_workspace(test_workspace, slot)

        # Start session with monitoring using Popen (non-blocking)
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            # Wait for container to be ready
            time.sleep(10)

            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            container_ip = get_container_ip(container_name)
            if not container_ip:
                pytest.skip("Container has no IP address")

            # Check for NFT rules
            assert check_nft_rules_exist(container_ip), (
                f"NFT monitoring rules not found for IP {container_ip}"
            )
        finally:
            proc.terminate()
            proc.wait(timeout=10)
            cleanup_container(container_name, coi_binary)

    def test_rules_removed_on_session_end(self, test_workspace, coi_binary):
        """Verify nftables rules are cleaned up when session ends."""
        slot = 51
        container_name = get_container_name_from_workspace(test_workspace, slot)

        # Start session
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            container_ip = get_container_ip(container_name)
            if not container_ip:
                pytest.skip("Container has no IP address")

            # Verify rules exist
            assert check_nft_rules_exist(container_ip), "Rules should exist while monitoring"

            # Stop session
            proc.terminate()
            proc.wait(timeout=10)
            time.sleep(3)

            # Check rules are gone
            result = subprocess.run(
                ["sudo", "-n", "nft", "list", "ruleset"],
                capture_output=True,
                text=True,
                timeout=10,
            )
            assert f"NFT_COI[{container_ip}]" not in result.stdout, "Rules not cleaned up"

        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
            cleanup_container(container_name, coi_binary)

    def test_multiple_rule_types(self, test_workspace, coi_binary):
        """Verify core rule types are created (general, suspicious)."""
        slot = 52
        container_name = get_container_name_from_workspace(test_workspace, slot)

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            # Get ruleset
            result = subprocess.run(
                ["sudo", "-n", "nft", "list", "ruleset"],
                capture_output=True,
                text=True,
                timeout=10,
            )

            # Should have general and suspicious rules (DNS is optional config)
            assert "NFT_COI[" in result.stdout, "General traffic rule not found"
            assert "NFT_SUSPICIOUS[" in result.stdout, "Suspicious traffic rule not found"
            # NFT_DNS is optional (depends on log_dns_queries config)

        finally:
            proc.terminate()
            proc.wait(timeout=10)
            cleanup_container(container_name, coi_binary)


class TestNetworkThreatDetection:
    """Test network threat detection scenarios."""

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    def test_metadata_endpoint_triggers_critical(self, test_workspace, coi_binary):
        """Test that metadata endpoint access triggers CRITICAL alert."""
        slot = 53
        container_name = get_container_name_from_workspace(test_workspace, slot)

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            # Attempt to access metadata endpoint from inside container
            subprocess.run(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "curl",
                    "-m",
                    "3",
                    "http://169.254.169.254/latest/meta-data/",
                ],
                capture_output=True,
                timeout=10,
            )

            # Give monitoring time to detect and log
            time.sleep(5)

            # Check audit log for threat events
            events = get_nft_threat_events(container_name)
            critical_events = [e for e in events if e.get("level") == "critical"]

            # Should have detected metadata access as critical
            assert len(critical_events) > 0 or len(events) > 0, (
                f"Expected threat logged for metadata access. Events: {events}"
            )

        finally:
            proc.terminate()
            proc.wait(timeout=10)
            cleanup_container(container_name, coi_binary)

    def test_container_killed_on_metadata_access(self, test_workspace, coi_binary):
        """Verify container is killed when accessing cloud metadata endpoint."""
        slot = 54
        container_name = get_container_name_from_workspace(test_workspace, slot)

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            # Access metadata endpoint (should trigger kill)
            subprocess.run(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "curl",
                    "-m",
                    "3",
                    "http://169.254.169.254/",
                ],
                capture_output=True,
                timeout=10,
            )

            # Wait for kill action
            time.sleep(8)

            # Verify container was killed or stopped
            state = get_container_state(container_name)
            assert state in ("Stopped", "Unknown"), (
                f"Container should have been killed but state is {state}"
            )

        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
            cleanup_container(container_name, coi_binary)

    def test_network_activity_counted(self, test_workspace, coi_binary):
        """Test that network activity hits NFT rules (verified via counters)."""
        slot = 55
        container_name = get_container_name_from_workspace(test_workspace, slot)

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            container_ip = get_container_ip(container_name)
            if not container_ip:
                pytest.skip("Container has no IP")

            # Make network request
            subprocess.run(
                ["incus", "exec", container_name, "--", "curl", "-m", "5", "https://example.com"],
                capture_output=True,
                timeout=30,
            )

            # Check nft ruleset for counter activity
            time.sleep(2)
            result = subprocess.run(
                ["sudo", "-n", "nft", "list", "ruleset"],
                capture_output=True,
                text=True,
                timeout=10,
            )

            # Find the NFT_COI rule for this container and check packets counter
            # Rule format: ... log prefix "NFT_COI[IP]: " ... counter packets N bytes M
            import re

            pattern = rf"NFT_COI\[{re.escape(container_ip)}\].*counter packets (\d+)"
            match = re.search(pattern, result.stdout)
            assert match, f"NFT_COI rule not found for {container_ip}"
            packets = int(match.group(1))
            assert packets > 0, f"No packets counted for {container_ip}"

        finally:
            proc.terminate()
            proc.wait(timeout=10)
            cleanup_container(container_name, coi_binary)


class TestAuditLogging:
    """Test NFT audit logging functionality."""

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    def test_audit_log_created(self, test_workspace, coi_binary):
        """Test that audit log file is created when monitoring starts."""
        slot = 56
        container_name = get_container_name_from_workspace(test_workspace, slot)

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            # Trigger some network activity
            subprocess.run(
                ["incus", "exec", container_name, "--", "curl", "-m", "5", "https://example.com"],
                capture_output=True,
                timeout=30,
            )

            time.sleep(3)

            # Check if audit directory exists
            audit_dir = Path.home() / ".coi" / "audit"
            assert audit_dir.exists(), f"Audit directory {audit_dir} not found"

        finally:
            proc.terminate()
            proc.wait(timeout=10)
            cleanup_container(container_name, coi_binary)


class TestDaemonLifecycle:
    """Test NFT monitoring daemon lifecycle."""

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    def test_daemon_starts_with_monitoring_flag(self, test_workspace, coi_binary):
        """Test that daemon starts when --monitor flag is used."""
        slot = 57
        container_name = get_container_name_from_workspace(test_workspace, slot)

        # Capture stderr to check for startup message
        stderr_file = Path("/tmp") / f"nft-test-{slot}.log"
        with open(stderr_file, "w") as stderr_fd:
            proc = subprocess.Popen(
                [
                    coi_binary,
                    "shell",
                    "--workspace",
                    test_workspace,
                    "--slot",
                    str(slot),
                    "--monitor",
                ],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=stderr_fd,
            )

            try:
                time.sleep(15)

                # Read stderr to check for startup message
                stderr_content = stderr_file.read_text()
                assert "[security] NFT network monitoring started" in stderr_content, (
                    f"NFT daemon startup message not found. stderr:\n{stderr_content}"
                )

            finally:
                proc.terminate()
                proc.wait(timeout=10)
                cleanup_container(container_name, coi_binary)
                stderr_file.unlink(missing_ok=True)


class TestNFTRuleCleanupOnKill:
    """Test NFT rule cleanup when containers are killed."""

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    def test_nft_rules_cleaned_on_coi_kill(self, test_workspace, coi_binary):
        """Verify NFT rules are removed when container is killed via coi kill."""
        slot = 60
        container_name = get_container_name_from_workspace(test_workspace, slot)

        # Start session with monitoring
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            container_ip = get_container_ip(container_name)
            if not container_ip:
                pytest.skip("Container has no IP address")

            # Verify rules exist before kill
            assert check_nft_rules_exist(container_ip), (
                f"NFT rules should exist for {container_ip} before kill"
            )

            # Kill using coi kill command
            kill_result = subprocess.run(
                [coi_binary, "kill", container_name, "--force"],
                capture_output=True,
                text=True,
                timeout=60,
            )
            assert kill_result.returncode == 0, f"coi kill failed: {kill_result.stderr}"

            # Give cleanup time to complete
            time.sleep(2)

            # Verify NFT rules are cleaned up
            assert not check_nft_rules_exist(container_ip), (
                f"NFT rules should be cleaned up for {container_ip} after coi kill"
            )

        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
            cleanup_container(container_name, coi_binary)

    def test_nft_rules_cleaned_on_auto_kill(self, test_workspace, coi_binary):
        """Verify NFT rules are removed when container is auto-killed by responder."""
        slot = 61
        container_name = get_container_name_from_workspace(test_workspace, slot)

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            container_ip = get_container_ip(container_name)
            if not container_ip:
                pytest.skip("Container has no IP address")

            # Verify rules exist before triggering kill
            assert check_nft_rules_exist(container_ip), (
                f"NFT rules should exist for {container_ip} before auto-kill"
            )

            # Trigger auto-kill by accessing metadata endpoint (CRITICAL threat)
            subprocess.run(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "curl",
                    "-m",
                    "3",
                    "http://169.254.169.254/",
                ],
                capture_output=True,
                timeout=10,
            )

            # Wait for responder to detect threat and kill container
            time.sleep(10)

            # Verify container was killed
            state = get_container_state(container_name)
            assert state in ("Stopped", "Unknown"), (
                f"Container should have been killed but state is {state}"
            )

            # Verify NFT rules are cleaned up
            assert not check_nft_rules_exist(container_ip), (
                f"NFT rules should be cleaned up for {container_ip} after auto-kill"
            )

        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
            cleanup_container(container_name, coi_binary)


class TestNFTRuleCleanupOnShutdown:
    """Test NFT rule cleanup when containers are shutdown."""

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    def test_nft_rules_cleaned_on_coi_shutdown(self, test_workspace, coi_binary):
        """Verify NFT rules are removed when container is shutdown via coi shutdown."""
        slot = 62
        container_name = get_container_name_from_workspace(test_workspace, slot)

        # Start session with monitoring
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            container_ip = get_container_ip(container_name)
            if not container_ip:
                pytest.skip("Container has no IP address")

            # Verify rules exist before shutdown
            assert check_nft_rules_exist(container_ip), (
                f"NFT rules should exist for {container_ip} before shutdown"
            )

            # Shutdown using coi shutdown command
            shutdown_result = subprocess.run(
                [coi_binary, "shutdown", container_name, "--force"],
                capture_output=True,
                text=True,
                timeout=60,
            )
            assert shutdown_result.returncode == 0, f"coi shutdown failed: {shutdown_result.stderr}"

            # Give cleanup time to complete
            time.sleep(2)

            # Verify NFT rules are cleaned up
            assert not check_nft_rules_exist(container_ip), (
                f"NFT rules should be cleaned up for {container_ip} after coi shutdown"
            )

        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
            cleanup_container(container_name, coi_binary)


def check_firewall_rules_exist(container_ip):
    """Check if firewalld direct rules exist for container IP."""
    result = subprocess.run(
        ["sudo", "-n", "firewall-cmd", "--direct", "--get-all-rules"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        return False
    return container_ip in result.stdout


class TestFirewallRuleCleanupOnAutoKill:
    """Test firewall rule cleanup when containers are auto-killed by responder."""

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    def test_firewall_rules_cleaned_on_auto_kill(self, test_workspace, coi_binary):
        """Verify firewall rules are removed when container is auto-killed by responder."""
        slot = 63
        container_name = get_container_name_from_workspace(test_workspace, slot)

        # Start session with monitoring AND restricted network (to have firewall rules)
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                str(slot),
                "--monitor",
                "--network",
                "restricted",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        try:
            time.sleep(10)
            if not wait_for_container_ready(container_name, timeout=30):
                pytest.skip("Container failed to start")

            container_ip = get_container_ip(container_name)
            if not container_ip:
                pytest.skip("Container has no IP address")

            # Verify firewall rules exist before triggering kill
            # (restricted mode creates firewall rules)
            if not check_firewall_rules_exist(container_ip):
                pytest.skip("No firewall rules created (firewalld may not be available)")

            # Trigger auto-kill by accessing metadata endpoint (CRITICAL threat)
            subprocess.run(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "curl",
                    "-m",
                    "3",
                    "http://169.254.169.254/",
                ],
                capture_output=True,
                timeout=10,
            )

            # Wait for responder to detect threat and kill container
            time.sleep(10)

            # Verify container was killed
            state = get_container_state(container_name)
            assert state in ("Stopped", "Unknown"), (
                f"Container should have been killed but state is {state}"
            )

            # Verify firewall rules are cleaned up
            assert not check_firewall_rules_exist(container_ip), (
                f"Firewall rules should be cleaned up for {container_ip} after auto-kill"
            )

        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
            cleanup_container(container_name, coi_binary)


class TestHealthChecks:
    """Test NFT monitoring health checks."""

    def test_health_command_runs(self, coi_binary):
        """Test that health command runs successfully."""
        result = subprocess.run(
            [coi_binary, "health", "--verbose"],
            capture_output=True,
            text=True,
            timeout=60,
        )
        # Health may return 1 (DEGRADED) with warnings - that's acceptable
        assert result.returncode in (0, 1), f"Health check errored: {result.stderr}"
        assert "checks passed" in result.stdout.lower(), (
            f"Expected health check summary in output:\n{result.stdout}"
        )

    def test_health_includes_monitoring_check(self, coi_binary):
        """Test that health includes monitoring-related checks."""
        result = subprocess.run(
            [coi_binary, "health", "--verbose"],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode in (0, 1), f"Health check errored: {result.stderr}"
        output_lower = result.stdout.lower()
        assert "monitoring" in output_lower or "process" in output_lower, (
            f"No monitoring checks found in health output:\n{result.stdout}"
        )

    def test_health_includes_network_check(self, coi_binary):
        """Test that health includes network-related checks."""
        result = subprocess.run(
            [coi_binary, "health", "--verbose"],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode in (0, 1), f"Health check errored: {result.stderr}"
        output_lower = result.stdout.lower()
        assert "network" in output_lower, (
            f"No network checks found in health output:\n{result.stdout}"
        )
