"""
Integration tests verifying host-side /proc/<pid>/net/{tcp,udp} network
connection detection for suspicious outbound connections.

The monitor reads /proc/<container-init-pid>/net/tcp[6] and udp[6] from the
host (namespaced via init PID), so an in-container attacker cannot blind it.

Tests open long-lived sockets inside the container so the connection stays
visible in /proc/net/* across multiple poll cycles, then assert that the
monitoring daemon emits a "network" category threat event.
"""

import hashlib
import json
import os
import subprocess
import time
from pathlib import Path

import pytest

# ── helpers ──────────────────────────────────────────────────────────────────


def _container_name(workspace: str) -> str:
    """Derive container name from workspace path (matches Go naming logic)."""
    abs_path = os.path.abspath(workspace)
    digest = hashlib.sha256(abs_path.encode()).hexdigest()[:8]
    return f"coi-{digest}-1"


def _container_state(name: str) -> str:
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


def _wait_running(name: str, timeout: int = 60) -> bool:
    for _ in range(timeout):
        if _container_state(name) == "Running":
            return True
        time.sleep(1)
    return False


def _get_threat_events(container_name: str) -> list[dict]:
    audit_file = Path.home() / ".coi" / "audit" / f"{container_name}.jsonl"
    if not audit_file.exists():
        return []
    events = []
    with audit_file.open() as fh:
        for line in fh:
            line = line.strip()
            if line:
                try:
                    event = json.loads(line)
                    if "level" in event:
                        events.append(event)
                except json.JSONDecodeError:
                    pass
    return events


def _poll_network_threats(container_name: str, max_wait: int = 40, extra_check=None) -> list[dict]:
    deadline = time.time() + max_wait
    while time.time() < deadline:
        events = _get_threat_events(container_name)
        net = [e for e in events if e.get("category") == "network"]
        if net and (extra_check is None or extra_check(net)):
            return net
        time.sleep(2)
    return []


def _cleanup(name: str, coi_binary: str) -> None:
    subprocess.run(
        [coi_binary, "container", "delete", name, "--force"],
        timeout=30,
        check=False,
    )


# ── shared monitoring config ──────────────────────────────────────────────────

_MONITORING_CONFIG = """
[network]
mode = "open"

[monitoring]
enabled = true
auto_pause_on_high = false
auto_kill_on_critical = false
poll_interval_sec = 2
file_read_threshold_mb = 100000.0
file_read_rate_mb_per_sec = 100000.0
audit_log_retention_days = 30
"""


@pytest.fixture
def _monitoring_enabled():
    """Write monitoring config to ~/.coi/config.toml, restore on teardown."""
    config_path = Path.home() / ".coi" / "config.toml"
    backup = config_path.read_text() if config_path.exists() else None
    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(_MONITORING_CONFIG)
    yield
    if backup is not None:
        config_path.write_text(backup)
    elif config_path.exists():
        config_path.unlink()


# ── tests ─────────────────────────────────────────────────────────────────────


class TestNetworkConnectionDetection:
    """Host-side /proc/<pid>/net detection of suspicious outbound connections."""

    def test_suspicious_tcp_port_detected(self, coi_binary, tmp_path, _monitoring_enabled):
        """TCP socket to C2 port 4444 held in SYN_SENT triggers network threat."""
        workspace = str(tmp_path / "workspace")
        os.makedirs(workspace, exist_ok=True)
        container_name = _container_name(workspace)

        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", workspace, "--slot", "1"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        if not _wait_running(container_name):
            proc.terminate()
            pytest.skip(f"Container {container_name} not ready")

        try:
            # Open a non-blocking TCP socket to 8.8.8.8:4444 and keep it alive.
            # The socket stays in SYN_SENT and remains visible in /proc/net/tcp
            # across poll cycles even though the connection never completes.
            tcp_cmd = (
                'python3 -c "'
                "import socket, time; "
                "s = socket.socket(socket.AF_INET, socket.SOCK_STREAM); "
                "s.setblocking(False); "
                "s.connect_ex(('8.8.8.8', 4444)); "
                "time.sleep(120)"
                '"'
            )
            subprocess.Popen(
                ["incus", "exec", container_name, "--", "bash", "-c", tcp_cmd],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            net_events = _poll_network_threats(container_name)

            assert len(net_events) > 0, (
                f"Expected network threat for TCP:4444, got none.\n"
                f"All events: {_get_threat_events(container_name)}"
            )
            assert "4444" in json.dumps(net_events), (
                f"Expected port 4444 in threat evidence, got:\n{json.dumps(net_events)}"
            )
        finally:
            _cleanup(container_name, coi_binary)

    def test_suspicious_udp_port_detected(self, coi_binary, tmp_path, _monitoring_enabled):
        """UDP socket connected to port 4444 triggers network threat."""
        workspace = str(tmp_path / "workspace")
        os.makedirs(workspace, exist_ok=True)
        container_name = _container_name(workspace)

        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", workspace, "--slot", "1"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        if not _wait_running(container_name):
            proc.terminate()
            pytest.skip(f"Container {container_name} not ready")

        try:
            # UDP connect() sets the kernel peer address so the socket appears
            # in /proc/net/udp with the remote addr set; send() sends a packet
            # so the entry stays active.
            udp_cmd = (
                'python3 -c "'
                "import socket, time; "
                "s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); "
                "s.connect(('8.8.8.8', 4444)); "
                "s.send(b'x'); "
                "time.sleep(120)"
                '"'
            )
            subprocess.Popen(
                ["incus", "exec", container_name, "--", "bash", "-c", udp_cmd],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            net_events = _poll_network_threats(container_name)

            assert len(net_events) > 0, (
                f"Expected network threat for UDP:4444, got none.\n"
                f"All events: {_get_threat_events(container_name)}"
            )
            assert "4444" in json.dumps(net_events), (
                f"Expected port 4444 in threat evidence, got:\n{json.dumps(net_events)}"
            )
        finally:
            _cleanup(container_name, coi_binary)

    def test_metadata_endpoint_detected(self, coi_binary, tmp_path, _monitoring_enabled):
        """TCP SYN to 169.254.169.254 triggers critical network threat."""
        workspace = str(tmp_path / "workspace")
        os.makedirs(workspace, exist_ok=True)
        container_name = _container_name(workspace)

        proc = subprocess.Popen(
            [coi_binary, "shell", "--workspace", workspace, "--slot", "1"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        if not _wait_running(container_name):
            proc.terminate()
            pytest.skip(f"Container {container_name} not ready")

        try:
            meta_cmd = (
                'python3 -c "'
                "import socket, time; "
                "s = socket.socket(socket.AF_INET, socket.SOCK_STREAM); "
                "s.setblocking(False); "
                "s.connect_ex(('169.254.169.254', 80)); "
                "time.sleep(120)"
                '"'
            )
            subprocess.Popen(
                ["incus", "exec", container_name, "--", "bash", "-c", meta_cmd],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            net_events = _poll_network_threats(
                container_name,
                extra_check=lambda evts: any(e.get("level") == "critical" for e in evts),
            )

            assert len(net_events) > 0, (
                f"Expected network threat for metadata endpoint, got none.\n"
                f"All events: {_get_threat_events(container_name)}"
            )
            critical = [e for e in net_events if e.get("level") == "critical"]
            assert len(critical) > 0, (
                f"Expected CRITICAL level for metadata endpoint, got:\n"
                f"{json.dumps(net_events, indent=2)}"
            )
        finally:
            _cleanup(container_name, coi_binary)
