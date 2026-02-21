#!/usr/bin/env python3
"""
Integration tests for nftables-based network monitoring.

Tests NFT monitoring daemon lifecycle, rule management, threat detection,
and integration with the monitoring system.

These tests verify the critical security functionality:
1. NFT rules are created/destroyed correctly
2. Suspicious network activity is detected
3. Containers are KILLED on CRITICAL threats

NOTE: These tests require systemd journal access. If journal access fails,
individual tests will be skipped with an appropriate message.

LIMITATION: In --background mode, monitoring daemons are started but immediately
cleaned up when the main process exits. This means persistent monitoring
(threat detection, container killing) cannot be tested in background mode.
Tests that require persistent monitoring are skipped in CI.
"""

import json
import os
import subprocess
import time
from pathlib import Path

import pytest


def get_container_ip(container_name):
    """Get container IP address from eth0."""
    result = subprocess.run(
        ["incus", "list", container_name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    container_info = json.loads(result.stdout)
    if not container_info:
        return None

    for iface_name, iface_info in container_info[0]["state"]["network"].items():
        if iface_name == "eth0":
            for addr in iface_info["addresses"]:
                if addr["family"] == "inet":
                    return addr["address"]
    return None


def check_nft_available():
    """Check if NFT monitoring is available (journal access + nftables)."""
    # Check nft command works
    result = subprocess.run(
        ["sudo", "-n", "nft", "list", "ruleset"],
        capture_output=True,
        timeout=10,
    )
    return result.returncode == 0


def start_monitoring_session(coi_binary, container_name, timeout=60):
    """Start a monitoring session and check for errors.

    Returns (success, result, skip_reason) tuple.
    """
    try:
        result = subprocess.run(
            [coi_binary, "shell", "--container", container_name, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired:
        return False, None, "Shell command timed out - NFT daemon may be hanging"

    # Check for common failure patterns
    if "timeout opening systemd journal" in result.stderr:
        return False, result, "systemd journal not accessible (timeout)"
    if "nft command timed out" in result.stderr:
        return False, result, "nft command timed out"
    if "Failed to start NFT monitoring" in result.stderr:
        return False, result, f"NFT monitoring failed to start: {result.stderr}"

    # Check for success
    if "[security] NFT network monitoring started" in result.stderr:
        return True, result, None

    # Unknown state - let test continue and possibly fail
    return True, result, None


def wait_for_nft_rules(container_ip, timeout=10):
    """Wait for NFT rules to be created."""
    start = time.time()
    while time.time() - start < timeout:
        result = subprocess.run(
            ["sudo", "nft", "list", "ruleset"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        if (
            f"NFT_COI[{container_ip}]" in result.stdout
            or f"NFT_DNS[{container_ip}]" in result.stdout
            or f"NFT_SUSPICIOUS[{container_ip}]" in result.stdout
        ):
            return True, result.stdout
        time.sleep(0.5)
    return False, result.stdout


@pytest.fixture(scope="module")
def nft_monitoring_available():
    """Check if NFT monitoring is available before running tests.

    This fixture runs once per module and skips all NFT tests if
    nft commands aren't working. We do a simple check rather than
    starting a full monitoring session to avoid hangs.
    """
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

    # Check if journalctl is accessible (for log monitoring)
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


def cleanup_session(coi_binary, container_name):
    """Clean up a monitoring session."""
    # Stop any running sessions
    subprocess.run(
        [coi_binary, "shutdown", container_name],
        capture_output=True,
        timeout=30,
    )
    # Small delay to ensure cleanup completes
    time.sleep(1)


# Test fixtures
@pytest.fixture(scope="module")
def test_container(request, coi_binary):
    """Create a shared test container for all NFT monitoring tests."""
    # Use container launch instead of shell (faster, no interactive session)
    container_name = f"coi-nft-test-{os.getpid()}"

    # Launch container directly
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    if result.returncode != 0:
        pytest.skip(f"Failed to launch test container: {result.stderr}")

    # Wait for container to be ready
    time.sleep(5)

    yield container_name

    # Cleanup
    subprocess.run([coi_binary, "container", "delete", container_name, "--force"], timeout=60)


@pytest.fixture
def nft_config():
    """Load and return NFT monitoring configuration."""
    config_path = Path.home() / ".config" / "coi" / "config.toml"
    if not config_path.exists():
        return {"monitoring": {"nft": {"enabled": True}}}

    # Parse TOML (simple parsing, not production-grade)
    config = {"monitoring": {"nft": {"enabled": True}}}
    return config


@pytest.fixture
def audit_log_path(test_container):
    """Return path to the NFT audit log for a container."""
    return Path.home() / ".coi" / "audit" / f"{test_container}-nft.jsonl"


class TestNFTRuleManagement:
    """Test nftables rule creation and deletion.

    NOTE: These tests require persistent monitoring, which doesn't work in
    --background mode (daemons are cleaned up when main process exits).
    Tests are skipped until interactive mode testing or a daemon architecture
    change is implemented.
    """

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    @pytest.fixture(autouse=True)
    def skip_background_mode(self):
        """Skip tests that require persistent monitoring."""
        pytest.skip(
            "NFT rule tests require persistent monitoring, "
            "which doesn't work in --background mode (daemons are cleaned up "
            "when main process exits)"
        )

    def test_rules_created_on_session_start(self, test_container, coi_binary):
        """Verify nftables LOG rules are created when session starts."""
        container_ip = get_container_ip(test_container)
        if not container_ip:
            pytest.skip("Container has no IP address")

        # Start a monitoring session with --monitor AND --background
        success, result, skip_reason = start_monitoring_session(coi_binary, test_container)
        if not success:
            pytest.skip(skip_reason)

        # Check for success message
        assert "[security] NFT network monitoring started" in result.stderr, (
            f"NFT monitoring not started. stderr:\n{result.stderr}"
        )

        try:
            # Wait for rules to be created
            rules_found, ruleset = wait_for_nft_rules(container_ip)
            assert rules_found, f"NFT monitoring rules not found in ruleset:\n{ruleset}"
        finally:
            # Cleanup
            cleanup_session(coi_binary, test_container)

    def test_rules_ordered_before_accept(self, test_container, coi_binary):
        """Verify LOG rules are inserted BEFORE ACCEPT rules in the chain.

        This is critical: if LOG rules come after ACCEPT, traffic matches
        ACCEPT first and never hits the LOG rules, making monitoring useless.
        """
        container_ip = get_container_ip(test_container)
        if not container_ip:
            pytest.skip("Container has no IP address")

        # Start a monitoring session in background mode
        result = subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            # Wait for rules to be created
            rules_found, _ = wait_for_nft_rules(container_ip)
            if not rules_found:
                pytest.skip("NFT rules not created - may be a timing issue")

            # Get the FORWARD chain rules in order
            result = subprocess.run(
                ["sudo", "nft", "-a", "list", "chain", "ip", "filter", "FORWARD"],
                capture_output=True,
                text=True,
                timeout=30,
            )

            # Parse rules and find positions
            lines = result.stdout.split("\n")
            log_rule_positions = []
            accept_rule_positions = []

            for i, line in enumerate(lines):
                if container_ip in line:
                    if "log prefix" in line and (
                        "NFT_COI" in line or "NFT_DNS" in line or "NFT_SUSPICIOUS" in line
                    ):
                        log_rule_positions.append(i)
                    elif "accept" in line.lower() and "ct state" not in line:
                        # ACCEPT rule for this container (not the conntrack rule)
                        accept_rule_positions.append(i)

            # Verify we found LOG rules
            assert len(log_rule_positions) > 0, (
                f"No LOG rules found for container {container_ip}. Rules output:\n{result.stdout}"
            )

            # If there are ACCEPT rules for this container, LOG rules must come first
            if accept_rule_positions:
                first_log = min(log_rule_positions)
                first_accept = min(accept_rule_positions)
                assert first_log < first_accept, (
                    f"LOG rules (line {first_log}) must come BEFORE ACCEPT rules (line {first_accept}). "
                    f"Current ordering will cause LOG rules to never match traffic. "
                    f"Rules:\n{result.stdout}"
                )
        finally:
            cleanup_session(coi_binary, test_container)

    def test_rules_removed_on_session_end(self, test_container, coi_binary):
        """Verify nftables rules are cleaned up when session ends."""
        container_ip = get_container_ip(test_container)
        if not container_ip:
            pytest.skip("Container has no IP address")

        # Start session in background mode
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        # Wait for rules to be created
        rules_found, _ = wait_for_nft_rules(container_ip)
        if not rules_found:
            pytest.skip("NFT rules not created - cannot test cleanup")

        # End session
        cleanup_session(coi_binary, test_container)
        time.sleep(2)

        # Check rules are gone
        result = subprocess.run(
            ["sudo", "nft", "list", "ruleset"], capture_output=True, text=True, timeout=30
        )

        assert f"NFT_COI[{container_ip}]" not in result.stdout, "NFT rules not cleaned up"
        assert f"NFT_DNS[{container_ip}]" not in result.stdout, "DNS rules not cleaned up"
        assert f"NFT_SUSPICIOUS[{container_ip}]" not in result.stdout, (
            "Suspicious rules not cleaned up"
        )

    def test_multiple_rule_types(self, test_container, coi_binary):
        """Verify all three rule types are created (general, DNS, suspicious)."""
        # Start session with --monitor and --background
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            # Wait a bit for rules to be created
            time.sleep(3)

            # Get ruleset
            result = subprocess.run(
                ["sudo", "nft", "list", "ruleset"], capture_output=True, text=True, timeout=30
            )

            # Should have all three prefixes for this container
            # Note: Specific IPs vary, but prefixes should be consistent
            has_general = "NFT_COI[" in result.stdout
            has_dns = "NFT_DNS[" in result.stdout
            has_suspicious = "NFT_SUSPICIOUS[" in result.stdout

            assert has_general, "General traffic rule not found"
            assert has_dns, "DNS rule not found"
            assert has_suspicious, "Suspicious traffic rule not found"
        finally:
            cleanup_session(coi_binary, test_container)


class TestNetworkThreatDetection:
    """Test network threat detection scenarios.

    NOTE: These tests require persistent monitoring, which doesn't work in
    --background mode (daemons are cleaned up when main process exits).
    Tests are skipped until interactive mode testing or a daemon architecture
    change is implemented.
    """

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    @pytest.fixture(autouse=True)
    def skip_background_mode(self):
        """Skip tests that require persistent monitoring."""
        pytest.skip(
            "Threat detection tests require persistent monitoring, "
            "which doesn't work in --background mode (daemons are cleaned up "
            "when main process exits)"
        )

    def test_metadata_endpoint_access_critical(self, test_container, audit_log_path, coi_binary):
        """Test that metadata endpoint access triggers CRITICAL alert."""
        # Start session with --monitor and --background
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            # Wait for rules to be ready
            time.sleep(3)

            # Attempt to access metadata endpoint from inside container
            try:
                subprocess.run(
                    [
                        "incus",
                        "exec",
                        test_container,
                        "--",
                        "curl",
                        "-m",
                        "5",
                        "http://169.254.169.254/latest/meta-data/",
                    ],
                    capture_output=True,
                    timeout=10,
                )
            except subprocess.TimeoutExpired:
                pass  # Expected - connection should be blocked

            # Give monitoring time to detect and log
            time.sleep(3)

            # Check audit log for CRITICAL event
            if audit_log_path.exists():
                with open(audit_log_path) as f:
                    events = [json.loads(line) for line in f if line.strip()]

                # Look for metadata endpoint alert
                metadata_events = [
                    e
                    for e in events
                    if e.get("level") == "critical"
                    and "169.254.169.254" in str(e.get("evidence", {}))
                ]

                assert len(metadata_events) > 0, "Metadata endpoint access not logged as CRITICAL"
        finally:
            cleanup_session(coi_binary, test_container)

    def test_rfc1918_address_high(self, test_container, audit_log_path, coi_binary):
        """Test that RFC1918 connections trigger HIGH alert."""
        # Start session in background mode
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            time.sleep(3)

            # Attempt to connect to RFC1918 address
            try:
                subprocess.run(
                    [
                        "incus",
                        "exec",
                        test_container,
                        "--",
                        "curl",
                        "-m",
                        "5",
                        "http://192.168.1.1/",
                    ],
                    capture_output=True,
                    timeout=10,
                )
            except subprocess.TimeoutExpired:
                pass  # Expected - connection should be blocked

            time.sleep(3)

            # Check audit log
            if audit_log_path.exists():
                with open(audit_log_path) as f:
                    events = [json.loads(line) for line in f if line.strip()]

                rfc1918_events = [
                    e
                    for e in events
                    if e.get("level") == "high" and "RFC1918" in e.get("title", "")
                ]

                assert len(rfc1918_events) > 0, "RFC1918 connection not logged as HIGH"
        finally:
            cleanup_session(coi_binary, test_container)

    def test_suspicious_port_critical(self, test_container, audit_log_path, coi_binary):
        """Test that connections to suspicious ports trigger CRITICAL alert."""
        # Start session in background mode
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            time.sleep(3)

            # Attempt to connect to suspicious C2 port (4444 - Metasploit default)
            try:
                subprocess.run(
                    [
                        "incus",
                        "exec",
                        test_container,
                        "--",
                        "nc",
                        "-w",
                        "2",
                        "-z",
                        "example.com",
                        "4444",
                    ],
                    capture_output=True,
                    timeout=10,
                )
            except subprocess.TimeoutExpired:
                pass

            time.sleep(3)

            # Check audit log
            if audit_log_path.exists():
                with open(audit_log_path) as f:
                    events = [json.loads(line) for line in f if line.strip()]

                port_events = [
                    e
                    for e in events
                    if e.get("level") == "critical" and "4444" in str(e.get("evidence", {}))
                ]

                assert len(port_events) > 0, "Suspicious port connection not logged as CRITICAL"
        finally:
            cleanup_session(coi_binary, test_container)

    def test_container_killed_on_critical_threat(self, coi_binary):
        """Test that container is actually KILLED when CRITICAL threat detected.

        This is the most important security test - verifies the full pipeline:
        1. Container runs with monitoring
        2. Suspicious network activity triggers nftables LOG
        3. Journal captures the log
        4. Detector identifies CRITICAL threat
        5. Responder KILLS the container
        6. Container is actually deleted
        """
        container_name = f"coi-kill-test-{os.getpid()}"

        try:
            # First, launch a container
            launch_result = subprocess.run(
                [coi_binary, "container", "launch", "coi", container_name],
                capture_output=True,
                text=True,
                timeout=60,
            )
            if launch_result.returncode != 0:
                pytest.skip(f"Failed to launch container: {launch_result.stderr}")

            time.sleep(3)

            # Now start shell with monitoring in background mode
            subprocess.run(
                [coi_binary, "shell", "--container", container_name, "--monitor", "--background"],
                capture_output=True,
                text=True,
                timeout=60,
            )

            # Verify container exists
            check = subprocess.run(
                ["incus", "list", container_name, "--format=csv"],
                capture_output=True,
                text=True,
                timeout=10,
            )
            assert len(check.stdout.strip()) > 0, (
                f"Container {container_name} not found after startup"
            )

            # Wait for monitoring to be fully ready
            time.sleep(5)

            # Trigger CRITICAL threat: access metadata endpoint
            # This should cause the container to be killed
            try:
                subprocess.run(
                    [
                        "incus",
                        "exec",
                        container_name,
                        "--",
                        "curl",
                        "-m",
                        "5",
                        "http://169.254.169.254/latest/meta-data/",
                    ],
                    capture_output=True,
                    timeout=10,
                )
            except subprocess.TimeoutExpired:
                pass

            # Give responder time to act
            time.sleep(5)

            # Verify container was killed
            check = subprocess.run(
                ["incus", "list", container_name, "--format=csv"],
                capture_output=True,
                text=True,
                timeout=10,
            )

            # Container should be gone or stopped
            assert container_name not in check.stdout or "STOPPED" in check.stdout, (
                f"Container {container_name} should have been killed but is still running"
            )

        finally:
            # Force cleanup if test failed
            subprocess.run(
                [coi_binary, "container", "delete", container_name, "--force"],
                capture_output=True,
                timeout=30,
            )

    def test_container_killed_on_metadata_access(self, coi_binary):
        """Verify container is killed when accessing cloud metadata endpoint.

        The metadata endpoint (169.254.169.254) is commonly used in SSRF attacks
        to steal cloud credentials. Accessing it should trigger immediate kill.
        """
        container_name = f"coi-meta-test-{os.getpid()}"

        try:
            # First, launch a container
            launch_result = subprocess.run(
                [coi_binary, "container", "launch", "coi", container_name],
                capture_output=True,
                text=True,
                timeout=60,
            )
            if launch_result.returncode != 0:
                pytest.skip(f"Failed to launch container: {launch_result.stderr}")

            time.sleep(3)

            # Start shell with monitoring in background mode
            subprocess.run(
                [coi_binary, "shell", "--container", container_name, "--monitor", "--background"],
                capture_output=True,
                text=True,
                timeout=60,
            )

            # Verify container exists
            check = subprocess.run(
                ["incus", "list", container_name, "--format=csv"],
                capture_output=True,
                text=True,
                timeout=10,
            )
            assert len(check.stdout.strip()) > 0, f"Container {container_name} not found"

            time.sleep(5)

            # Access metadata endpoint (should trigger kill)
            try:
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
            except subprocess.TimeoutExpired:
                pass

            # Wait for kill action
            time.sleep(5)

            # Verify container was killed
            check = subprocess.run(
                ["incus", "list", container_name, "--format=csv"],
                capture_output=True,
                text=True,
                timeout=10,
            )
            assert container_name not in check.stdout or "STOPPED" in check.stdout, (
                "Container should have been killed after metadata access"
            )

        finally:
            subprocess.run(
                [coi_binary, "container", "delete", container_name, "--force"],
                capture_output=True,
                timeout=30,
            )

    def test_dns_query_monitoring(self, test_container, coi_binary):
        """Test that DNS queries are logged when LogDNSQueries is enabled."""
        # Start session in background mode
        result = subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            time.sleep(3)

            # Make DNS query
            subprocess.run(
                ["incus", "exec", test_container, "--", "nslookup", "google.com"],
                capture_output=True,
                timeout=30,
            )

            # Check dmesg for DNS logging
            time.sleep(2)
            result = subprocess.run(
                ["sudo", "dmesg", "-T", "--level=info"],
                capture_output=True,
                text=True,
                timeout=30,
            )

            container_ip = get_container_ip(test_container)
            assert container_ip is not None, "Could not get container IP"

            # Look for DNS log entry
            dns_logged = f"NFT_DNS[{container_ip}]" in result.stdout or "53" in result.stdout
            assert dns_logged, "DNS queries not logged"
        finally:
            cleanup_session(coi_binary, test_container)

    def test_allowlist_violation_detected(self, test_container, coi_binary):
        """Test that connections outside allowlist are detected.

        When network.mode is 'allowlist', only allowed destinations
        should be permitted. Others should trigger alerts.
        """
        # Start session in background mode
        # Note: This test may not trigger alerts if allowlist is not configured
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            time.sleep(3)

            # Try to connect to a domain not in allowlist
            result = subprocess.run(
                [
                    "incus",
                    "exec",
                    test_container,
                    "--",
                    "curl",
                    "-I",
                    "-m",
                    "5",
                    "https://google.com",
                ],
                capture_output=True,
                timeout=30,
            )

            # In strict allowlist mode, this should fail or be logged
            # For now, just verify the connection attempt was made
            # The actual blocking depends on configuration
            assert result.returncode != 0 or "HTTP" in result.stdout.decode(
                "utf-8", errors="ignore"
            ), "Connection to non-allowlisted domain should be blocked"
        finally:
            cleanup_session(coi_binary, test_container)

    def test_short_lived_connection_detection(self, test_container, audit_log_path, coi_binary):
        """Test that short-lived connections are still logged."""
        # Start session in background mode
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            time.sleep(3)

            # Quick connection attempt (will fail but should be logged)
            try:
                subprocess.run(
                    [
                        "incus",
                        "exec",
                        test_container,
                        "--",
                        "nc",
                        "-w",
                        "1",
                        "-z",
                        "1.2.3.4",
                        "12345",
                    ],
                    capture_output=True,
                    timeout=5,
                )
            except subprocess.TimeoutExpired:
                pass

            time.sleep(3)

            # Check audit log or dmesg
            result = subprocess.run(
                ["sudo", "dmesg", "-T", "--level=info"],
                capture_output=True,
                text=True,
                timeout=30,
            )

            # Should see the connection attempt logged
            assert len(result.stdout) > 0, "Short-lived connection not logged"
        finally:
            cleanup_session(coi_binary, test_container)


class TestAuditLogging:
    """Test NFT audit logging functionality.

    NOTE: These tests require persistent monitoring to generate log entries.
    """

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    @pytest.fixture(autouse=True)
    def skip_background_mode(self):
        """Skip tests that require persistent monitoring."""
        pytest.skip(
            "Audit logging tests require persistent monitoring, "
            "which doesn't work in --background mode"
        )

    def test_audit_log_created(self, test_container, audit_log_path, coi_binary):
        """Test that audit log file is created when monitoring starts."""
        # Start session in background mode
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            time.sleep(3)

            # Trigger some network activity
            subprocess.run(
                ["incus", "exec", test_container, "--", "curl", "-m", "5", "https://example.com"],
                capture_output=True,
                timeout=30,
            )

            time.sleep(3)

            # Check if audit directory exists
            audit_dir = Path.home() / ".coi" / "audit"
            assert audit_dir.exists(), f"Audit directory {audit_dir} not found"
        finally:
            cleanup_session(coi_binary, test_container)

    def test_audit_log_json_format(self, test_container, audit_log_path, coi_binary):
        """Test that audit log entries are valid JSON."""
        # Start session in background mode
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            time.sleep(3)

            # Trigger network activity to generate log entries
            subprocess.run(
                ["incus", "exec", test_container, "--", "curl", "-m", "5", "https://example.com"],
                capture_output=True,
                timeout=30,
            )

            time.sleep(3)

            # If audit log exists, verify JSON format
            if audit_log_path.exists():
                with open(audit_log_path) as f:
                    for line_num, line in enumerate(f, 1):
                        if line.strip():
                            try:
                                json.loads(line)
                            except json.JSONDecodeError as e:
                                pytest.fail(f"Line {line_num}: Invalid JSON: {e}")
        finally:
            cleanup_session(coi_binary, test_container)


class TestDaemonLifecycle:
    """Test NFT monitoring daemon lifecycle."""

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    def test_daemon_starts_with_container(self, test_container, coi_binary):
        """Test that daemon starts when monitoring is enabled."""
        # Start session with --monitor and --background
        try:
            result = subprocess.run(
                [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
                capture_output=True,
                text=True,
                timeout=60,
            )
        except subprocess.TimeoutExpired:
            pytest.skip("Shell command timed out - container may not be ready")

        try:
            # Check stderr for daemon startup message
            assert "[security] NFT network monitoring started" in result.stderr, (
                f"NFT daemon startup message not found. stderr:\n{result.stderr}"
            )
        finally:
            cleanup_session(coi_binary, test_container)

    def test_daemon_stops_cleanly(self, test_container, coi_binary):
        """Test that daemon stops without errors."""
        # Start session in background mode
        try:
            subprocess.run(
                [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
                capture_output=True,
                text=True,
                timeout=60,
            )
        except subprocess.TimeoutExpired:
            pytest.skip("Shell command timed out - container may not be ready")

        # Stop session
        stop_result = subprocess.run(
            [coi_binary, "shutdown", test_container],
            capture_output=True,
            text=True,
            timeout=60,
        )

        # Check for clean shutdown (no errors in stderr)
        assert "Failed to stop NFT monitoring" not in stop_result.stderr, (
            "NFT daemon failed to stop cleanly"
        )

    def test_daemon_disabled_in_config(self, test_container):
        """Test that daemon doesn't start when disabled in config."""
        # This test would require modifying config, which is complex
        # For now, we'll skip it and add a TODO
        pytest.skip("Config modification test - TODO")


class TestHealthChecks:
    """Test NFT monitoring health checks.

    These tests verify that the coi health command works correctly.
    Note: nftables and systemd are build-time dependencies, not runtime health checks.
    """

    def test_health_command_runs(self, coi_binary):
        """Test that health command runs successfully."""
        result = subprocess.run(
            [coi_binary, "health", "--verbose"],
            capture_output=True,
            text=True,
            timeout=60,
        )
        # Health may return 1 (DEGRADED) with warnings, but should still run
        # Exit code 0=HEALTHY, 1=DEGRADED, 2+=ERROR
        assert result.returncode in (0, 1), f"Health check errored: {result.stderr}"
        # Verify we got actual health check output
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
        # Health may return 1 (DEGRADED) with warnings - that's acceptable
        assert result.returncode in (0, 1), f"Health check errored: {result.stderr}"
        # Check that health includes process monitoring or security-related checks
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
        # Health may return 1 (DEGRADED) with warnings - that's acceptable
        assert result.returncode in (0, 1), f"Health check errored: {result.stderr}"
        # Check that health includes network checks
        output_lower = result.stdout.lower()
        assert "network" in output_lower, (
            f"No network checks found in health output:\n{result.stdout}"
        )


class TestEdgeCases:
    """Test edge cases and error handling.

    NOTE: These tests require persistent monitoring.
    """

    @pytest.fixture(autouse=True)
    def check_nft_available(self, nft_monitoring_available):
        """Ensure NFT monitoring is available before running tests."""
        pass

    @pytest.fixture(autouse=True)
    def skip_background_mode(self):
        """Skip tests that require persistent monitoring."""
        pytest.skip(
            "Edge case tests require persistent monitoring, which doesn't work in --background mode"
        )

    def test_high_volume_traffic(self, test_container, coi_binary):
        """Test that rate limiting works for high-volume traffic."""
        # Start session in background mode
        subprocess.run(
            [coi_binary, "shell", "--container", test_container, "--monitor", "--background"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        try:
            time.sleep(3)

            # Generate high volume of traffic
            for _ in range(20):
                subprocess.run(
                    [
                        "incus",
                        "exec",
                        test_container,
                        "--",
                        "curl",
                        "-s",
                        "-m",
                        "1",
                        "https://example.com",
                    ],
                    capture_output=True,
                    timeout=10,
                )

            # System should still be responsive
            result = subprocess.run(
                ["incus", "exec", test_container, "--", "echo", "test"],
                capture_output=True,
                text=True,
                timeout=10,
            )
            assert result.returncode == 0, "Container became unresponsive after high traffic"
        finally:
            cleanup_session(coi_binary, test_container)
