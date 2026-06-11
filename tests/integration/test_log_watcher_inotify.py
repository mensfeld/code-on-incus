#!/usr/bin/env python3
"""
Integration tests for LogWatcher inotify-based detection.

These tests verify behaviors that are specific to the inotify implementation:
- syslog is watched in addition to auth.log
- log rotation (mv + new file) is handled transparently
- events are delivered promptly (within 3 s) rather than on a 5 s polling cycle

All tests write suspicious lines directly into the container via incus exec
(not via SSH) so the exact write time is known.
"""

import hashlib
import json
import os
import subprocess
import time
from pathlib import Path

import pytest


# ---------------------------------------------------------------------------
# Shared helpers (mirrors test_security_monitoring.py)
# ---------------------------------------------------------------------------


def get_container_name_from_workspace(workspace):
    abs_path = os.path.abspath(workspace)
    hash_digest = hashlib.sha256(abs_path.encode()).hexdigest()[:8]
    return f"coi-{hash_digest}-1"


def wait_for_container_running(name, timeout=30):
    for _ in range(timeout):
        result = subprocess.run(
            ["incus", "list", name, "--format=json"],
            capture_output=True, text=True, timeout=10
        )
        if result.returncode == 0:
            containers = json.loads(result.stdout)
            if containers and containers[0].get("status") == "Running":
                return True
        time.sleep(1)
    return False


def get_threat_events(container_name):
    log_path = Path.home() / ".coi" / "audit" / f"{container_name}.jsonl"
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
    subprocess.run(
        [coi_binary, "container", "delete", name, "--force"],
        timeout=30, check=False
    )


def _monitoring_config(auto_pause=False):
    return f"""
[network]
mode = "open"

[monitoring]
enabled = true
auto_pause_on_high = {"true" if auto_pause else "false"}
auto_kill_on_critical = false
poll_interval_sec = 1
file_read_threshold_mb = 500
file_read_rate_mb_per_sec = 1000
process_count_threshold = 9999
process_spawn_rate_threshold = 9999
"""


@pytest.fixture
def test_workspace(tmp_path):
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    (workspace / "README.md").write_text("# Test")
    return str(workspace)


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


class TestLogWatcherInotify:
    """Integration tests for inotify-driven log file monitoring."""

    def test_syslog_event_detected(self, test_workspace, coi_binary):
        """Writing a suspicious line to syslog (not auth.log) is detected.

        Verifies that both log candidates are actively watched — the existing
        TestAuthLogWatcher suite only exercises auth.log.
        """
        config_path = Path.home() / ".coi" / "config.toml"
        backup = config_path.read_text() if config_path.exists() else None
        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(_monitoring_config())

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-66"
        )
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(test_workspace), "--slot", "66"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL
        )

        try:
            assert wait_for_container_running(container_name), (
                f"Container {container_name} did not start"
            )
            time.sleep(3)  # wait for monitoring daemon to start

            subprocess.run(
                [
                    "incus", "exec", container_name, "--",
                    "bash", "-c",
                    "mkdir -p /var/log && "
                    "echo 'Jun  5 12:00:00 coi sshd[1234]: Failed password for attacker"
                    " from 1.2.3.4 port 22222 ssh2' >> /var/log/syslog"
                ],
                check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
            )

            auth_events = []
            for _ in range(30):
                events = get_threat_events(container_name)
                auth_events = [
                    e for e in events
                    if e.get("category") == "auth"
                    and e.get("evidence", {}).get("auth_log", {}).get("pattern")
                    == "ssh_failed_password"
                ]
                if auth_events:
                    break
                time.sleep(1)

            assert len(auth_events) > 0, (
                f"Expected auth threat from syslog, got events: {events}"
            )
            assert auth_events[0].get("level") == "warning"
        finally:
            proc.terminate()
            if backup:
                config_path.write_text(backup)
            elif config_path.exists():
                config_path.unlink()
            cleanup_container(container_name, coi_binary)

    def test_event_detected_promptly(self, test_workspace, coi_binary):
        """Auth log threat is detected within 3 seconds of the line being written.

        With 5-second polling the detection could take up to 5 s; with inotify
        it arrives within ~1 s. The 3 s threshold distinguishes the two reliably.
        """
        config_path = Path.home() / ".coi" / "config.toml"
        backup = config_path.read_text() if config_path.exists() else None
        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(_monitoring_config())

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-67"
        )
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(test_workspace), "--slot", "67"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL
        )

        try:
            assert wait_for_container_running(container_name), (
                f"Container {container_name} did not start"
            )
            time.sleep(3)

            write_time = time.monotonic()
            subprocess.run(
                [
                    "incus", "exec", container_name, "--",
                    "bash", "-c",
                    "mkdir -p /var/log && "
                    "echo 'Jun  5 12:00:00 coi sshd[99]: Failed password for attacker"
                    " from 5.6.7.8 port 22222 ssh2' >> /var/log/auth.log"
                ],
                check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
            )

            auth_events = []
            deadline = write_time + 3.0
            while time.monotonic() < deadline:
                events = get_threat_events(container_name)
                auth_events = [
                    e for e in events
                    if e.get("category") == "auth"
                    and e.get("evidence", {}).get("auth_log", {}).get("pattern")
                    == "ssh_failed_password"
                ]
                if auth_events:
                    break
                time.sleep(0.2)

            elapsed = time.monotonic() - write_time
            assert len(auth_events) > 0, (
                f"Expected auth threat within 3 s (inotify), elapsed {elapsed:.1f}s. "
                f"Events: {get_threat_events(container_name)}"
            )
        finally:
            proc.terminate()
            if backup:
                config_path.write_text(backup)
            elif config_path.exists():
                config_path.unlink()
            cleanup_container(container_name, coi_binary)

    def test_log_rotation_handled(self, test_workspace, coi_binary):
        """Events are detected in a new auth.log file after log rotation.

        Simulates logrotate behaviour: auth.log is renamed to auth.log.1 and a
        new auth.log is created. The inotify directory watch (IN_CREATE) should
        trigger re-registration of the new file so subsequent writes are still
        detected.
        """
        config_path = Path.home() / ".coi" / "config.toml"
        backup = config_path.read_text() if config_path.exists() else None
        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(_monitoring_config())

        container_name = (
            get_container_name_from_workspace(str(test_workspace)).rsplit("-", 1)[0] + "-68"
        )
        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", str(test_workspace), "--slot", "68"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL
        )

        try:
            assert wait_for_container_running(container_name), (
                f"Container {container_name} did not start"
            )
            time.sleep(3)

            # Write first suspicious line into the original auth.log.
            subprocess.run(
                [
                    "incus", "exec", container_name, "--",
                    "bash", "-c",
                    "mkdir -p /var/log && "
                    "echo 'Jun  5 12:00:00 coi sshd[1]: Failed password for attacker"
                    " from 1.1.1.1 port 22 ssh2' >> /var/log/auth.log"
                ],
                check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
            )

            # Wait for the first event before rotating.
            first_events = []
            for _ in range(15):
                events = get_threat_events(container_name)
                first_events = [
                    e for e in events if e.get("category") == "auth"
                ]
                if first_events:
                    break
                time.sleep(1)

            assert len(first_events) > 0, (
                f"Expected first auth threat before rotation, events: {events}"
            )

            # Simulate logrotate: move auth.log aside and create a fresh one.
            subprocess.run(
                [
                    "incus", "exec", container_name, "--",
                    "bash", "-c",
                    "mv /var/log/auth.log /var/log/auth.log.1 && touch /var/log/auth.log"
                ],
                check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
            )

            # Give the inotify directory watch time to re-register the new file.
            time.sleep(1)

            # Write a second suspicious line into the rotated-in new auth.log.
            subprocess.run(
                [
                    "incus", "exec", container_name, "--",
                    "bash", "-c",
                    "echo 'Jun  5 12:00:01 coi sshd[2]: Failed password for attacker"
                    " from 2.2.2.2 port 22 ssh2' >> /var/log/auth.log"
                ],
                check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
            )

            # Poll for a second distinct event (different source IP in evidence).
            second_events = []
            for _ in range(30):
                events = get_threat_events(container_name)
                second_events = [
                    e for e in events
                    if e.get("category") == "auth"
                    and "2.2.2.2" in e.get("description", "")
                ]
                if second_events:
                    break
                time.sleep(1)

            assert len(second_events) > 0, (
                f"Expected auth threat from post-rotation auth.log, "
                f"events after rotation: {get_threat_events(container_name)}"
            )
        finally:
            proc.terminate()
            if backup:
                config_path.write_text(backup)
            elif config_path.exists():
                config_path.unlink()
            cleanup_container(container_name, coi_binary)
