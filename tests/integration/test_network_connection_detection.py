"""
Integration tests verifying host-side /proc/<pid>/net/{tcp,udp} network
connection detection for suspicious outbound connections.

The monitor reads /proc/<container-init-pid>/net/tcp[6] and udp[6] from the
host (namespaced via init PID), so an in-container attacker cannot blind it.

Tests open long-lived sockets inside the container so the connection stays
visible in /proc/net/* across multiple poll cycles, then assert that the
monitoring daemon emits a "network" category threat event.
"""

import json
import subprocess
import time
from pathlib import Path


# Fast poll so tests don't wait long; must match the config written below.
_POLL_INTERVAL_SEC = 2
_MAX_WAIT_SEC = 40

# Config shared by all tests: monitoring on, no auto-response (so the
# container stays alive for assertions), open network mode (avoids CI
# false-positives from nftables firewall rules).
_MONITORING_CONFIG = f"""
[monitoring]
enabled = true
auto_pause_on_high = false
auto_kill_on_critical = false
poll_interval_sec = {_POLL_INTERVAL_SEC}
file_read_threshold_mb = 100000.0
file_read_rate_mb_per_sec = 100000.0
audit_log_retention_days = 30

[network]
mode = "open"
"""


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
                    events.append(json.loads(line))
                except json.JSONDecodeError:
                    pass
    return events


def _wait_for_container_running(container_name: str, timeout: int = 60) -> bool:
    deadline = time.time() + timeout
    while time.time() < deadline:
        result = subprocess.run(
            ["incus", "list", container_name, "--format=csv", "-c", "s"],
            capture_output=True,
            text=True,
        )
        if "RUNNING" in result.stdout:
            return True
        time.sleep(1)
    return False


def _poll_for_network_threat(container_name: str, extra_check=None) -> list[dict]:
    deadline = time.time() + _MAX_WAIT_SEC
    while time.time() < deadline:
        events = _get_threat_events(container_name)
        network_events = [e for e in events if e.get("category") == "network"]
        if network_events:
            if extra_check is None or extra_check(network_events):
                return network_events
        time.sleep(_POLL_INTERVAL_SEC)
    return []


class TestNetworkConnectionDetection:
    """Host-side /proc/<pid>/net detection of suspicious outbound connections."""

    def test_suspicious_tcp_port_detected(self, coi_binary, tmp_path, dummy_image):
        """TCP socket to C2 port 4444 held in SYN_SENT triggers network threat."""
        workspace = tmp_path / "workspace"
        workspace.mkdir()
        config_file = tmp_path / "config.toml"
        config_file.write_text(_MONITORING_CONFIG)

        env_extra = {"COI_CONFIG": str(config_file)}
        import os

        env = {**os.environ, **env_extra}

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--background",
                "--workspace",
                str(workspace),
                "--image",
                dummy_image,
            ],
            capture_output=True,
            text=True,
            env=env,
        )
        stdout, _ = proc.communicate(timeout=120)

        container_name = None
        for line in stdout.splitlines():
            if "Container:" in line:
                container_name = line.split("Container:")[-1].strip()
                break

        assert container_name, f"Could not find container name in output:\n{stdout}"
        assert _wait_for_container_running(container_name), (
            f"Container {container_name} did not reach RUNNING state"
        )

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
        )

        network_events = _poll_for_network_threat(container_name)

        try:
            assert len(network_events) > 0, (
                f"Expected network threat for TCP:4444, got none.\n"
                f"All events: {_get_threat_events(container_name)}"
            )
            event_text = json.dumps(network_events)
            assert "4444" in event_text, (
                f"Expected port 4444 in threat evidence, got:\n{event_text}"
            )
        finally:
            subprocess.run(
                ["incus", "delete", "--force", container_name],
                capture_output=True,
            )

    def test_suspicious_udp_port_detected(self, coi_binary, tmp_path, dummy_image):
        """UDP socket connected to port 4444 triggers network threat (udp read added)."""
        workspace = tmp_path / "workspace"
        workspace.mkdir()
        config_file = tmp_path / "config.toml"
        config_file.write_text(_MONITORING_CONFIG)

        import os

        env = {**os.environ, "COI_CONFIG": str(config_file)}

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--background",
                "--workspace",
                str(workspace),
                "--image",
                dummy_image,
            ],
            capture_output=True,
            text=True,
            env=env,
        )
        stdout, _ = proc.communicate(timeout=120)

        container_name = None
        for line in stdout.splitlines():
            if "Container:" in line:
                container_name = line.split("Container:")[-1].strip()
                break

        assert container_name, f"Could not find container name in output:\n{stdout}"
        assert _wait_for_container_running(container_name), (
            f"Container {container_name} did not reach RUNNING state"
        )

        # UDP connect() sets the kernel's peer address so the socket appears
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
        )

        network_events = _poll_for_network_threat(container_name)

        try:
            assert len(network_events) > 0, (
                f"Expected network threat for UDP:4444, got none.\n"
                f"All events: {_get_threat_events(container_name)}"
            )
            event_text = json.dumps(network_events)
            assert "4444" in event_text, (
                f"Expected port 4444 in threat evidence, got:\n{event_text}"
            )
        finally:
            subprocess.run(
                ["incus", "delete", "--force", container_name],
                capture_output=True,
            )

    def test_metadata_endpoint_detected(self, coi_binary, tmp_path, dummy_image):
        """TCP SYN to 169.254.169.254 triggers critical network threat regardless of mode."""
        workspace = tmp_path / "workspace"
        workspace.mkdir()
        config_file = tmp_path / "config.toml"
        config_file.write_text(_MONITORING_CONFIG)

        import os

        env = {**os.environ, "COI_CONFIG": str(config_file)}

        proc = subprocess.Popen(
            [
                coi_binary,
                "shell",
                "--background",
                "--workspace",
                str(workspace),
                "--image",
                dummy_image,
            ],
            capture_output=True,
            text=True,
            env=env,
        )
        stdout, _ = proc.communicate(timeout=120)

        container_name = None
        for line in stdout.splitlines():
            if "Container:" in line:
                container_name = line.split("Container:")[-1].strip()
                break

        assert container_name, f"Could not find container name in output:\n{stdout}"
        assert _wait_for_container_running(container_name), (
            f"Container {container_name} did not reach RUNNING state"
        )

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
        )

        def is_critical(events):
            return any(e.get("level") == "critical" for e in events)

        network_events = _poll_for_network_threat(container_name, extra_check=is_critical)

        try:
            assert len(network_events) > 0, (
                f"Expected network threat for metadata endpoint, got none.\n"
                f"All events: {_get_threat_events(container_name)}"
            )
            critical = [e for e in network_events if e.get("level") == "critical"]
            assert len(critical) > 0, (
                f"Expected CRITICAL level for metadata endpoint access, got:\n"
                f"{json.dumps(network_events, indent=2)}"
            )
        finally:
            subprocess.run(
                ["incus", "delete", "--force", container_name],
                capture_output=True,
            )
