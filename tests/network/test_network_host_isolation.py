"""
Integration tests for network host isolation.

Tests that containers cannot access host services in RESTRICTED and ALLOWLIST modes,
but that host can access container services (response traffic allowed).

Note: These tests require OVN networking (now configured in CI).
"""

import os
import subprocess
import tempfile
import time

import pytest

# Skip all tests in this module when running on bridge network (no OVN/ACL support)
pytestmark = pytest.mark.skipif(
    os.getenv("CI_NETWORK_TYPE") == "bridge",
    reason="Network mode tests require OVN networking (ACL support)",
)


def test_restricted_blocks_host_services(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that RESTRICTED mode blocks container access to RFC1918 private networks.

    Verifies that containers cannot reach RFC1918 addresses that should be blocked
    by the ACL rules (10.0.0.1, 172.16.0.1, 192.168.1.1).
    """
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

        # Test: attempt to curl RFC1918 addresses (should be blocked by ACL)
        for test_ip in ["10.0.0.1", "172.16.0.1", "192.168.1.1"]:
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
                    "3",
                    f"http://{test_ip}",
                ],
                capture_output=True,
                text=True,
                timeout=10,
            )

            # Connection should fail (blocked by RFC1918 ACL)
            assert result.returncode != 0, (
                f"Container should not reach RFC1918 address {test_ip}: {result.stderr}"
            )

    finally:
        os.unlink(config_file)


def test_allowlist_blocks_host_services(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that ALLOWLIST mode blocks container access to RFC1918 private networks.

    Verifies that containers cannot reach RFC1918 addresses even with
    permissive allowlist (RFC1918 blocking + default-deny protects the host).
    """
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

        # Test: attempt to curl RFC1918 addresses (should be blocked)
        for test_ip in ["10.0.0.1", "172.16.0.1", "192.168.1.1"]:
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
                    "3",
                    f"http://{test_ip}",
                ],
                capture_output=True,
                text=True,
                timeout=10,
            )

            # Connection should fail (blocked by RFC1918 + allowlist)
            assert result.returncode != 0, (
                f"Container should not reach RFC1918 address {test_ip}: {result.stderr}"
            )

    finally:
        os.unlink(config_file)


def test_host_can_access_container_services(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that host can access services running in containers.

    Verifies bidirectional network isolation: containers cannot reach host,
    but host can reach containers (response traffic allowed).
    """
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
