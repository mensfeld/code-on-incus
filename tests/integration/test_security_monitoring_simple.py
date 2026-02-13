#!/usr/bin/env python3
"""
Integration tests for security monitoring - Real use case testing.

Tests monitoring during actual coi shell sessions (not pre-created containers).
Reflects how users will actually use the feature: coi shell --monitor
"""

import json
import os
import subprocess
import time
from pathlib import Path

import pytest


@pytest.fixture
def test_workspace(tmp_path):
    """Create a temporary workspace for testing."""
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    # Create a test file
    (workspace / "test.txt").write_text("test content")
    return str(workspace)


@pytest.fixture
def monitoring_config():
    """Enable monitoring via config for tests."""
    config_path = Path.home() / ".config" / "coi" / "config.toml"
    config_backup = None

    # Backup existing config
    if config_path.exists():
        config_backup = config_path.read_text()

    # Write test config with monitoring enabled
    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(
        """
[monitoring]
enabled = true
auto_pause_on_high = true
auto_kill_on_critical = true
poll_interval_sec = 1

[monitoring.nft]
enabled = false
"""
    )

    yield config_path

    # Restore original config
    if config_backup:
        config_path.write_text(config_backup)
    elif config_path.exists():
        config_path.unlink()


def get_container_state(container_name):
    """Get container state."""
    result = subprocess.run(
        ["incus", "list", container_name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        return "Unknown"

    containers = json.loads(result.stdout)
    if not containers:
        return "Unknown"

    return containers[0].get("status", "Unknown")


def wait_for_container_state(container_name, expected_states, timeout=15):
    """Wait for container to reach expected state."""
    start = time.time()
    while time.time() - start < timeout:
        state = get_container_state(container_name)
        if state in expected_states:
            return state
        time.sleep(1)
    return get_container_state(container_name)


def get_audit_events(container_name):
    """Read audit log events for a container."""
    audit_log_path = Path.home() / ".coi" / "audit" / f"{container_name}.jsonl"

    if not audit_log_path.exists():
        return []

    events = []
    with open(audit_log_path) as f:
        for line in f:
            if line.strip():
                try:
                    event = json.loads(line)
                    # Only include threat events (have 'level' field)
                    if "level" in event:
                        events.append(event)
                except json.JSONDecodeError:
                    pass

    return events


class TestRealUsageMonitoring:
    """Test monitoring in real usage scenarios."""

    def test_basic_shell_with_monitoring(
        self, test_workspace, monitoring_config, coi_binary
    ):
        """Test that coi shell with monitoring starts successfully."""
        # Start shell with monitoring
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--debug"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        # Give it time to start
        time.sleep(5)

        # Check process is running
        assert proc.poll() is None, "Shell should still be running"

        # Exit cleanly
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=10)

    def test_harmless_command_no_alert(
        self, test_workspace, monitoring_config, coi_binary
    ):
        """Test that normal commands don't trigger alerts."""
        # Start monitored shell
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", test_workspace, "--debug"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        # Wait for shell to start
        time.sleep(5)

        # Get container name from stderr
        stderr_output = proc.stderr.read(1000)  # Read partial stderr
        container_name = None
        for line in stderr_output.split("\n"):
            if "Container:" in line:
                container_name = line.split("Container:")[1].strip()
                break

        assert container_name, "Could not find container name"

        # Run harmless commands
        proc.stdin.write("echo hello\n")
        proc.stdin.write("ls\n")
        proc.stdin.flush()

        # Wait for monitoring to poll
        time.sleep(3)

        # Exit
        proc.stdin.write("exit\n")
        proc.stdin.flush()
        proc.wait(timeout=10)

        # Check audit log - should have snapshots but no threat events
        events = get_audit_events(container_name)
        critical_events = [e for e in events if e.get("level") == "critical"]
        assert len(critical_events) == 0, "No threats should be detected"

        # Cleanup
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            timeout=30,
            check=False,
        )


# Note: More complex tests (reverse shells, env scanning) would follow this pattern
# but require more sophisticated command injection and detection verification.
# The key is they all start with real "coi shell" sessions.
