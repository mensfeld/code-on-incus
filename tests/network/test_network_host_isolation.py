"""
Integration tests for network host isolation.

Tests that containers cannot access host services in RESTRICTED and ALLOWLIST modes,
but that host can access container services (response traffic allowed).

Note: These tests require OVN networking (now configured in CI).
"""

import os
import socket
import subprocess
import tempfile
import threading
import time
from http.server import HTTPServer, SimpleHTTPRequestHandler

import pytest

# Skip all tests in this module when running on bridge network (no OVN/ACL support)
pytestmark = pytest.mark.skipif(
    os.getenv("CI_NETWORK_TYPE") == "bridge",
    reason="Network mode tests require OVN networking (ACL support)",
)


def get_host_private_ip():
    """Get the host's private IP address (for testing RFC1918 blocking)."""
    try:
        # Create a socket to determine which interface is used for internet access
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("8.8.8.8", 80))
        ip = s.getsockname()[0]
        s.close()
        return ip
    except Exception:
        # Fallback: try to get IP from hostname
        return socket.gethostbyname(socket.gethostname())


def start_http_server(port, stop_event):
    """Start a simple HTTP server on specified port in background thread."""

    class Handler(SimpleHTTPRequestHandler):
        def log_message(self, format, *args):
            pass  # Suppress logging

        def do_GET(self):
            self.send_response(200)
            self.send_header("Content-type", "text/plain")
            self.end_headers()
            self.wfile.write(b"Host HTTP Server")

    server = HTTPServer(("0.0.0.0", port), Handler)

    # Run server until stop event is set
    while not stop_event.is_set():
        server.handle_request()

    server.server_close()


def test_restricted_blocks_host_services(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that RESTRICTED mode blocks container access to host services.

    Verifies that containers cannot reach HTTP services running on the host's
    private IP (RFC1918 blocking protects the host).

    Note: This test is skipped in cloud/CI environments where host IP ranges
    may not be properly blocked by OVN ACLs due to networking configuration.
    """
    # Skip in CI environments where host isolation is complex
    if os.getenv("GITHUB_ACTIONS") or os.getenv("CI"):
        pytest.skip("Skipping host isolation test in CI environment")

    # Get host IP
    host_ip = get_host_private_ip()

    # Skip if host IP is not RFC1918 (test relies on RFC1918 blocking)
    if not (
        host_ip.startswith("10.") or host_ip.startswith("172.") or host_ip.startswith("192.168.")
    ):
        pytest.skip(f"Host IP {host_ip} is not RFC1918, cannot test host isolation")

    # Create temporary config with RESTRICTED mode
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "restricted"
""")
        config_file = f.name

    # Start HTTP server on host
    server_port = 8765
    stop_event = threading.Event()
    server_thread = threading.Thread(target=start_http_server, args=(server_port, stop_event))
    server_thread.daemon = True
    server_thread.start()

    # Give server time to start
    time.sleep(1)

    try:
        # Start container in background with RESTRICTED mode
        env = os.environ.copy()
        env["COI_CONFIG"] = config_file

        result = subprocess.run(
            [
                coi_binary,
                "shell",
                "--workspace",
                workspace_dir,
                "--network=restricted",
                "--background",
            ],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

        # Extract container name from output
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, f"Could not find container name in output: {output}"

        # Test: attempt to curl host service (should be blocked by RFC1918 ACL)
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "curl",
                "-s",
                "-m",
                "5",
                f"http://{host_ip}:{server_port}",
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )

        # Connection should fail (blocked by RFC1918 ACL)
        assert result.returncode != 0, (
            f"Container should not reach host service at {host_ip}:{server_port}: {result.stderr}"
        )

    finally:
        # Stop HTTP server
        stop_event.set()
        # Make a request to unblock handle_request()
        try:
            s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            s.connect(("localhost", server_port))
            s.close()
        except Exception:
            pass
        server_thread.join(timeout=2)
        os.unlink(config_file)


def test_allowlist_blocks_host_services(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that ALLOWLIST mode blocks container access to host services.

    Verifies that containers cannot reach HTTP services on the host even with
    permissive allowlist (RFC1918 blocking + default-deny protects the host).
    """
    # Get host IP
    host_ip = get_host_private_ip()

    # Skip if host IP is not RFC1918
    if not (
        host_ip.startswith("10.") or host_ip.startswith("172.") or host_ip.startswith("192.168.")
    ):
        pytest.skip(f"Host IP {host_ip} is not RFC1918, cannot test host isolation")

    # Create temporary config with ALLOWLIST mode
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",
]
refresh_interval_minutes = 30
""")
        config_file = f.name

    # Start HTTP server on host
    server_port = 8766
    stop_event = threading.Event()
    server_thread = threading.Thread(target=start_http_server, args=(server_port, stop_event))
    server_thread.daemon = True
    server_thread.start()

    # Give server time to start
    time.sleep(1)

    try:
        # Start container in background with ALLOWLIST mode
        env = os.environ.copy()
        env["COI_CONFIG"] = config_file

        result = subprocess.run(
            [
                coi_binary,
                "shell",
                "--workspace",
                workspace_dir,
                "--network=allowlist",
                "--background",
            ],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

        # Extract container name from output
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, f"Could not find container name in output: {output}"

        # Test: attempt to curl host service (should be blocked)
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "curl",
                "-s",
                "-m",
                "5",
                f"http://{host_ip}:{server_port}",
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )

        # Connection should fail (blocked by RFC1918 + allowlist)
        assert result.returncode != 0, (
            f"Container should not reach host service at {host_ip}:{server_port}: {result.stderr}"
        )

    finally:
        # Stop HTTP server
        stop_event.set()
        # Make a request to unblock handle_request()
        try:
            s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            s.connect(("localhost", server_port))
            s.close()
        except Exception:
            pass
        server_thread.join(timeout=2)
        os.unlink(config_file)


def test_host_can_access_container_services(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that host can access services running in containers.

    Verifies bidirectional network isolation: containers cannot reach host,
    but host can reach containers (response traffic allowed).

    Note: This test is skipped in CI environments where container networking
    topology may not allow direct host-to-container access.
    """
    # Skip in CI environments where container access is complex
    if os.getenv("GITHUB_ACTIONS") or os.getenv("CI"):
        pytest.skip("Skipping container access test in CI environment")
    # Create temporary config with RESTRICTED mode
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "restricted"
""")
        config_file = f.name

    try:
        # Start container in background with RESTRICTED mode
        env = os.environ.copy()
        env["COI_CONFIG"] = config_file

        result = subprocess.run(
            [
                coi_binary,
                "shell",
                "--workspace",
                workspace_dir,
                "--network=restricted",
                "--background",
            ],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

        # Extract container name from output
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, f"Could not find container name in output: {output}"

        # Get container IP address
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "hostname",
                "-I",
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )

        assert result.returncode == 0, f"Failed to get container IP: {result.stderr}"
        container_ip = result.stderr.strip().split()[0]

        # Start HTTP server in container
        server_proc = subprocess.Popen(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "python3",
                "-m",
                "http.server",
                "8080",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

        # Give server time to start
        time.sleep(3)

        try:
            # Test: host curls container service (should work - response traffic allowed)
            result = subprocess.run(
                ["curl", "-s", "-m", "5", f"http://{container_ip}:8080"],
                capture_output=True,
                text=True,
                timeout=10,
            )

            # Host should be able to reach container service
            assert result.returncode == 0, (
                f"Host should be able to reach container at {container_ip}:8080: {result.stderr}"
            )
            assert "Directory listing" in result.stdout or "Index of" in result.stdout, (
                f"Unexpected response from container service: {result.stdout}"
            )

        finally:
            # Stop HTTP server in container
            server_proc.terminate()
            server_proc.wait(timeout=5)

    finally:
        os.unlink(config_file)
