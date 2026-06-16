"""
Integration tests verifying host-side /proc/<pid>/net/{tcp,udp} network
connection detection for suspicious outbound connections.

The monitor reads /proc/<container-init-pid>/net/tcp[6] and udp[6] from the
host (namespaced via init PID), so an in-container attacker cannot blind it.

Tests create long-lived ESTABLISHED connections inside the container so the
connection is stable and visible in /proc/net/* across multiple poll cycles.

For C2-port tests (TCP/UDP to port 4444) we use the container's own IP so
there is always a reachable route and the connection reaches ESTABLISHED
state without relying on external network policy.

For the metadata-endpoint test we assign 169.254.169.254/32 to the loopback
interface inside the container and run a server there, then connect to it so
the socket reaches ESTABLISHED state (stable) rather than SYN_SENT (fragile).
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


def _get_container_ip(container_name: str) -> str:
    """Return the container's primary IPv4 address (eth0)."""
    result = subprocess.run(
        ["incus", "exec", container_name, "--", "hostname", "-I"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        return ""
    parts = result.stdout.strip().split()
    return parts[0] if parts else ""


def _wait_for_ip(container_name: str, timeout: int = 30) -> str:
    """Wait until the container has a routable IPv4 address and return it.

    A container can reach Running state before DHCP completes, so polling
    once immediately after _wait_running often returns an empty string.
    """
    for _ in range(timeout):
        ip = _get_container_ip(container_name)
        if ip:
            return ip
        time.sleep(1)
    return ""


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


def _poll_network_threats(container_name: str, max_wait: int = 75, extra_check=None) -> list[dict]:
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
        """TCP ESTABLISHED connection to C2 port 4444 triggers network threat.

        Uses the container's own IP to guarantee a routable destination and
        start an in-container server so the connection stays ESTABLISHED (not
        SYN_SENT which can disappear quickly if the network rejects the SYN).
        """
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
            # Get container's own IP so we can create a routable connection.
            # Use _wait_for_ip because DHCP may not have completed yet even
            # though the container is already in Running state.
            container_ip = _wait_for_ip(container_name)
            if not container_ip:
                pytest.skip(f"Could not get IP for {container_name}")

            # Start a TCP server on port 4444 inside the container (background).
            # Using python3 -m http.server so no extra packages are needed.
            subprocess.run(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "bash",
                    "-c",
                    "nohup python3 -m http.server 4444 --bind 0.0.0.0 </dev/null &>/dev/null &",
                ],
                capture_output=True,
                timeout=10,
            )
            # Poll until port 4444 is actually listening (up to 15 s).
            # A fixed sleep of 2 s is too short under CI load — python3's startup
            # can exceed it, leaving the connect() below with no server to reach,
            # which means the connection never becomes ESTABLISHED and is
            # never detected as a threat.
            for _ in range(15):
                ready = subprocess.run(
                    [
                        "incus",
                        "exec",
                        container_name,
                        "--",
                        "bash",
                        "-c",
                        "ss -tlnp 2>/dev/null | grep -q ':4444'",
                    ],
                    capture_output=True,
                    timeout=5,
                )
                if ready.returncode == 0:
                    break
                time.sleep(1)
            else:
                pytest.skip("TCP server on port 4444 did not start within 15 s")

            # Connect from inside the container to container_ip:4444.
            # The connection becomes ESTABLISHED and stays visible in
            # /proc/<init_pid>/net/tcp across every 2-second poll cycle.
            tcp_cmd = (
                'python3 -c "'
                "import socket, time; "
                f"s = socket.socket(socket.AF_INET, socket.SOCK_STREAM); "
                f"s.connect(('{container_ip}', 4444)); "
                'time.sleep(120)"'
            )
            subprocess.Popen(
                ["incus", "exec", container_name, "--", "bash", "-c", tcp_cmd],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            net_events = _poll_network_threats(container_name)

            assert len(net_events) > 0, (
                f"Expected network threat for TCP:4444, got none.\n"
                f"Container IP: {container_ip}\n"
                f"All events: {_get_threat_events(container_name)}"
            )
            assert "4444" in json.dumps(net_events), (
                f"Expected port 4444 in threat evidence, got:\n{json.dumps(net_events)}"
            )
        finally:
            _cleanup(container_name, coi_binary)

    def test_suspicious_udp_port_detected(self, coi_binary, tmp_path, _monitoring_enabled):
        """UDP socket connected to container's own port 4444 triggers network threat.

        Uses the container's own IP so connect() succeeds immediately (UDP
        connect just sets the peer address) and the socket is stable in
        /proc/net/udp for the duration of the test.
        """
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
            container_ip = _wait_for_ip(container_name)
            if not container_ip:
                pytest.skip(f"Could not get IP for {container_name}")

            # UDP connect() just sets the peer address; no server needed.
            # The socket appears in /proc/net/udp with remote = container_ip:4444
            # for the lifetime of the python process.
            udp_cmd = (
                'python3 -c "'
                "import socket, time; "
                "s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); "
                f"s.connect(('{container_ip}', 4444)); "
                "s.send(b'x'); "
                'time.sleep(120)"'
            )
            subprocess.Popen(
                ["incus", "exec", container_name, "--", "bash", "-c", udp_cmd],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            net_events = _poll_network_threats(container_name)

            assert len(net_events) > 0, (
                f"Expected network threat for UDP:4444, got none.\n"
                f"Container IP: {container_ip}\n"
                f"All events: {_get_threat_events(container_name)}"
            )
            assert "4444" in json.dumps(net_events), (
                f"Expected port 4444 in threat evidence, got:\n{json.dumps(net_events)}"
            )
        finally:
            _cleanup(container_name, coi_binary)

    def test_metadata_endpoint_detected(self, coi_binary, tmp_path, _monitoring_enabled):
        """TCP ESTABLISHED connection to 169.254.169.254 triggers critical threat.

        Assigns 169.254.169.254/32 to the container's loopback interface and
        starts an HTTP server there so the client socket reaches ESTABLISHED
        state (stable across poll cycles) rather than SYN_SENT (fragile — the
        SYN is dropped at the Incus bridge and the state times out quickly).
        """
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
            # Assign the metadata endpoint IP to the loopback interface so we
            # can bind a server there and create a stable ESTABLISHED connection.
            subprocess.run(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "ip",
                    "addr",
                    "add",
                    "169.254.169.254/32",
                    "dev",
                    "lo",
                ],
                capture_output=True,
                timeout=10,
            )

            # Start an HTTP server bound to the metadata endpoint IP.
            subprocess.run(
                [
                    "incus",
                    "exec",
                    container_name,
                    "--",
                    "bash",
                    "-c",
                    "nohup python3 -m http.server 8080 --bind 169.254.169.254 </dev/null &>/dev/null &",
                ],
                capture_output=True,
                timeout=10,
            )
            # Give the server a moment to start listening.
            time.sleep(2)

            # Connect TCP to 169.254.169.254:8080 → ESTABLISHED, stable in
            # /proc/<init_pid>/net/tcp for the full duration of the test.
            meta_cmd = (
                'python3 -c "'
                "import socket, time; "
                "s = socket.socket(socket.AF_INET, socket.SOCK_STREAM); "
                "s.connect(('169.254.169.254', 8080)); "
                'time.sleep(120)"'
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
