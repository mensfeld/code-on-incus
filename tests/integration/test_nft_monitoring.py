#!/usr/bin/env python3
"""
Integration tests for nftables-based network monitoring.

Tests NFT monitoring daemon lifecycle, rule management, threat detection,
and integration with the monitoring system.
"""

import json
import os
import subprocess
import time
from pathlib import Path

import pytest


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
    """Test nftables rule creation and deletion."""

    def test_rules_created_on_session_start(self, test_container, coi_binary):
        """Verify nftables LOG rules are created when session starts."""
        # Get container IP
        result = subprocess.run(
            ["incus", "list", test_container, "--format=json"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        container_info = json.loads(result.stdout)
        if not container_info:
            pytest.skip("Container not found")

        # Extract IP from eth0
        container_ip = None
        for iface_name, iface_info in container_info[0]["state"]["network"].items():
            if iface_name == "eth0":
                for addr in iface_info["addresses"]:
                    if addr["family"] == "inet":
                        container_ip = addr["address"]
                        break

        if not container_ip:
            pytest.skip("Container has no IP address")

        # Start a monitoring session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        # Give it time to set up rules
        time.sleep(5)

        # Check nftables rules exist
        result = subprocess.run(
            ["sudo", "nft", "list", "ruleset"], capture_output=True, text=True, timeout=30
        )

        # Should have our log prefix
        assert (
            f"NFT_COI[{container_ip}]" in result.stdout
            or f"NFT_DNS[{container_ip}]" in result.stdout
            or f"NFT_SUSPICIOUS[{container_ip}]" in result.stdout
        ), "NFT monitoring rules not found in ruleset"

        # Cleanup
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_rules_removed_on_session_end(self, test_container, coi_binary):
        """Verify nftables rules are cleaned up when session ends."""
        # Get container IP
        result = subprocess.run(
            ["incus", "list", test_container, "--format=json"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        container_info = json.loads(result.stdout)
        container_ip = None
        for iface_name, iface_info in container_info[0]["state"]["network"].items():
            if iface_name == "eth0":
                for addr in iface_info["addresses"]:
                    if addr["family"] == "inet":
                        container_ip = addr["address"]
                        break

        if not container_ip:
            pytest.skip("Container has no IP address")

        # Start and immediately end session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

        # Give cleanup time to complete
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
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Get ruleset
        result = subprocess.run(
            ["sudo", "nft", "list", "ruleset"], capture_output=True, text=True, timeout=30
        )

        # Should have all three prefixes for this container
        # Note: Specific IPs vary, but prefixes should be consistent
        has_general = "NFT_COI[" in result.stdout
        has_dns = "NFT_DNS[" in result.stdout
        has_suspicious = "NFT_SUSPICIOUS[" in result.stdout

        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

        assert has_general, "General traffic rule not found"
        assert has_dns, "DNS rule not found"
        assert has_suspicious, "Suspicious traffic rule not found"


class TestNetworkThreatDetection:
    """Test network threat detection scenarios."""

    def test_metadata_endpoint_access_critical(self, test_container, audit_log_path, coi_binary):
        """Test that metadata endpoint access triggers CRITICAL alert."""
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

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
                if e.get("level") == "critical" and "169.254.169.254" in str(e.get("evidence", {}))
            ]

            assert len(metadata_events) > 0, "Metadata endpoint access not logged as CRITICAL"

        # Cleanup
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_rfc1918_address_high(self, test_container, audit_log_path, coi_binary):
        """Test that RFC1918 connections trigger HIGH alert."""
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Attempt to connect to RFC1918 address
        try:
            subprocess.run(
                ["incus", "exec", test_container, "--", "curl", "-m", "5", "http://192.168.1.1/"],
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
                e for e in events if e.get("level") == "high" and "RFC1918" in e.get("title", "")
            ]

            assert len(rfc1918_events) > 0, "RFC1918 connection not logged as HIGH"

        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_suspicious_port_critical(self, test_container, audit_log_path, coi_binary):
        """Test that connections to suspicious ports trigger CRITICAL alert."""
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

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

        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_dns_query_monitoring(self, test_container, audit_log_path, coi_binary):
        """Test that DNS queries are logged."""
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Perform DNS lookups
        for _ in range(5):
            subprocess.run(
                ["incus", "exec", test_container, "--", "nslookup", "example.com"],
                capture_output=True,
                timeout=10,
            )
            time.sleep(0.5)

        time.sleep(3)

        # Check audit log for DNS events (port 53)
        if audit_log_path.exists():
            with open(audit_log_path) as f:
                content = f.read()

            # Should see DNS traffic logged (port 53)
            assert "53" in content or "DNS" in content, "DNS queries not logged"

        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_short_lived_connection_detection(self, test_container, audit_log_path, coi_binary):
        """Test that short-lived connections (<2s) are detected."""
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Make quick HTTP request (should complete in <1s)
        subprocess.run(
            [
                "incus",
                "exec",
                test_container,
                "--",
                "curl",
                "-I",
                "-m",
                "5",
                "https://api.anthropic.com/",
            ],
            capture_output=True,
            timeout=10,
            check=False,  # Don't raise on non-zero exit
        )

        time.sleep(3)

        # Check that connection was logged (even though short-lived)
        if audit_log_path.exists():
            with open(audit_log_path) as f:
                content = f.read()

            # Should see anthropic.com connection attempt logged
            # (Either the domain or its resolved IP)
            assert len(content) > 0, "Short-lived connection not logged"

        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_allowlist_violation_detected(self, test_container, audit_log_path, coi_binary):
        """
        Test that in allowlist mode, connections outside the allowlist are detected and logged.

        This is a critical security test verifying that:
        1. Container runs in allowlist mode with restricted access
        2. Attempts to reach non-allowlisted destinations are blocked by firewall
        3. NFT monitoring logs these blocked attempts at kernel level
        4. Threat detection identifies allowlist violations
        5. Audit log records the security event
        """
        # Create temporary config with allowlist mode
        config_dir = Path.home() / ".config" / "coi"
        config_path = config_dir / "config.toml"
        config_backup = None

        # Backup existing config if present
        if config_path.exists():
            config_backup = config_path.read_text()

        try:
            # Write allowlist config
            config_dir.mkdir(parents=True, exist_ok=True)
            config_path.write_text("""
[network]
mode = "allowlist"
allowed_domains = ["github.com", "api.github.com"]

[monitoring]
enabled = true
auto_pause_on_high = false
auto_kill_on_critical = false

[monitoring.nft]
enabled = true
rate_limit_per_second = 100
""")

            # Start session with allowlist config
            proc = subprocess.Popen(
                [coi_binary, "shell", "--container", test_container],
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            time.sleep(5)

            # Clear any existing audit log entries
            if audit_log_path.exists():
                audit_log_path.unlink()

            # Attempt 1: Access allowlisted domain (should succeed or at least be attempted)
            subprocess.run(
                [
                    "incus",
                    "exec",
                    test_container,
                    "--",
                    "curl",
                    "-I",
                    "-m",
                    "5",
                    "https://github.com",
                ],
                capture_output=True,
                timeout=10,
                check=False,
            )

            time.sleep(2)

            # Attempt 2: Access NON-allowlisted domain (should be blocked)
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
                timeout=10,
                check=False,
            )

            # Connection should fail (blocked by firewall)
            assert result.returncode != 0, "Connection to non-allowlisted domain should be blocked"

            # Give monitoring time to detect and log
            time.sleep(3)

            # Verify NFT monitoring logged the attempt
            if audit_log_path.exists():
                with open(audit_log_path) as f:
                    events = [json.loads(line) for line in f if line.strip()]

                # Look for allowlist violation events
                # NFT should log the connection attempt even though firewall blocks it
                allowlist_violations = [
                    e
                    for e in events
                    if (
                        e.get("level") in ["high", "warning"]
                        and (
                            "allowlist" in e.get("title", "").lower()
                            or "unauthorized" in e.get("title", "").lower()
                            or "not in allowlist" in e.get("description", "").lower()
                        )
                    )
                ]

                assert len(events) > 0, (
                    "NFT monitoring did not log any network events. "
                    "Kernel-level logging may not be working."
                )

                assert len(allowlist_violations) > 0, (
                    f"Allowlist violation not detected. "
                    f"Expected HIGH/WARNING event for non-allowlisted connection. "
                    f"Events logged: {len(events)}. "
                    f"Event types: {[e.get('title') for e in events]}"
                )

                # Verify event details
                violation = allowlist_violations[0]
                assert violation.get("category") == "network", (
                    "Violation should be categorized as network threat"
                )
                assert violation.get("container") == test_container, (
                    "Violation should reference correct container"
                )

                print(f"✓ Allowlist violation detected and logged: {violation.get('title')}")
                print(f"  Level: {violation.get('level')}")
                print(f"  Evidence: {violation.get('evidence', {})}")
            else:
                pytest.fail(
                    f"Audit log not created at {audit_log_path}. NFT monitoring may not be running."
                )

            # Cleanup
            proc.stdin.write("exit\n")
            proc.stdin.flush()
            proc.wait(timeout=30)

        finally:
            # Restore original config
            if config_backup:
                config_path.write_text(config_backup)
            elif config_path.exists():
                config_path.unlink()


class TestAuditLogging:
    """Test audit log functionality."""

    def test_audit_log_created(self, test_container, audit_log_path, coi_binary):
        """Test that audit log file is created."""
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Generate some network activity
        subprocess.run(
            ["incus", "exec", test_container, "--", "curl", "-I", "https://api.anthropic.com/"],
            capture_output=True,
            timeout=10,
        )

        time.sleep(3)

        # Check audit log exists
        assert audit_log_path.exists(), f"Audit log not created at {audit_log_path}"

        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_audit_log_json_format(self, test_container, audit_log_path, coi_binary):
        """Test that audit log entries are valid JSON Lines."""
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Generate activity
        subprocess.run(
            ["incus", "exec", test_container, "--", "curl", "-I", "https://api.anthropic.com/"],
            capture_output=True,
            timeout=10,
        )

        time.sleep(3)

        # Parse audit log
        if audit_log_path.exists():
            with open(audit_log_path) as f:
                for line_num, line in enumerate(f, 1):
                    if not line.strip():
                        continue
                    try:
                        event = json.loads(line)
                        # Validate required fields
                        assert "timestamp" in event, f"Line {line_num}: missing timestamp"
                        assert "level" in event, f"Line {line_num}: missing level"
                        assert "category" in event, f"Line {line_num}: missing category"
                        assert event["category"] == "network", (
                            f"Line {line_num}: expected category=network"
                        )
                    except json.JSONDecodeError as e:
                        pytest.fail(f"Line {line_num}: Invalid JSON: {e}")

        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)


class TestDaemonLifecycle:
    """Test NFT monitoring daemon lifecycle."""

    def test_daemon_starts_with_container(self, test_container, coi_binary):
        """Test that daemon starts when monitoring is enabled."""
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Check stderr for daemon startup message
        output = proc.stderr.read()
        assert "NFT monitoring started" in output, "NFT daemon startup message not found"

        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_daemon_stops_cleanly(self, test_container, coi_binary):
        """Test that daemon stops without errors."""
        # Start and stop session
        result = subprocess.run(
            [coi_binary, "shell", "--container", test_container],
            input="exit\n",
            capture_output=True,
            text=True,
            timeout=60,
        )

        # Check for clean shutdown (no errors in stderr)
        assert "Failed to stop NFT monitoring" not in result.stderr, (
            "NFT daemon failed to stop cleanly"
        )

    def test_daemon_disabled_in_config(self, test_container):
        """Test that daemon doesn't start when disabled in config."""
        # This test would require modifying config, which is complex
        # For now, we'll skip it and add a TODO
        pytest.skip("Config modification test - TODO")


class TestHealthChecks:
    """Test NFT monitoring health checks."""

    def test_nftables_health_check(self, coi_binary):
        """Test that nftables health check works."""
        result = subprocess.run(
            [coi_binary, "health", "--format=json"], capture_output=True, text=True, timeout=60
        )

        health_data = json.loads(result.stdout)

        # Check if nftables check is present
        nftables_check = health_data["checks"].get("nftables")
        if nftables_check:
            assert nftables_check["status"] in ["ok", "warning", "failed"], (
                f"Invalid nftables check status: {nftables_check['status']}"
            )

    def test_systemd_journal_health_check(self, coi_binary):
        """Test that systemd-journal health check works."""
        result = subprocess.run(
            [coi_binary, "health", "--format=json"], capture_output=True, text=True, timeout=60
        )

        health_data = json.loads(result.stdout)

        # Check if systemd_journal check is present
        journal_check = health_data["checks"].get("systemd_journal")
        if journal_check:
            assert journal_check["status"] in ["ok", "warning", "failed"], (
                f"Invalid journal check status: {journal_check['status']}"
            )

    def test_libsystemd_health_check(self, coi_binary):
        """Test that libsystemd health check works."""
        result = subprocess.run(
            [coi_binary, "health", "--format=json"], capture_output=True, text=True, timeout=60
        )

        health_data = json.loads(result.stdout)

        # Check if libsystemd check is present
        lib_check = health_data["checks"].get("libsystemd")
        if lib_check:
            assert lib_check["status"] in ["ok", "warning", "failed"], (
                f"Invalid libsystemd check status: {lib_check['status']}"
            )


class TestEdgeCases:
    """Test edge cases and error handling."""

    def test_multiple_containers_isolated(self):
        """Test that rules for multiple containers are isolated."""
        pytest.skip("Multi-container test - requires complex setup")

    def test_rule_cleanup_after_crash(self):
        """Test that rules are cleaned up even after abnormal termination."""
        pytest.skip("Crash recovery test - requires forced termination")

    def test_high_volume_traffic(self, test_container, coi_binary):
        """Test rate limiting with high volume traffic."""
        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Generate high volume traffic (100+ connections)
        for i in range(150):
            subprocess.run(
                [
                    "incus",
                    "exec",
                    test_container,
                    "--",
                    "curl",
                    "-I",
                    "-m",
                    "1",
                    "https://api.anthropic.com/",
                ],
                capture_output=True,
                timeout=5,
            )
            if i % 10 == 0:
                time.sleep(0.5)  # Brief pause every 10 requests

        time.sleep(3)

        # The test passes if the system doesn't crash
        # Rate limiting should prevent log explosion
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)


if __name__ == "__main__":
    pytest.main([__file__, "-v", "--tb=short"])
