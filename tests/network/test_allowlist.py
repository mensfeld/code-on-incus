"""
Integration tests for network allowlist mode.

Tests the domain allowlisting feature with DNS resolution and IP-based filtering.

Network isolation is implemented using firewalld direct rules.
"""

import json
import os
import subprocess
import tempfile
import time


def test_allowlist_mode_allows_specified_domains(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that allowlist mode allows access to domains in allowed_domains.

    Verifies that containers can reach domains explicitly listed in the allowlist.
    """
    # Create temporary config with allowlist
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",                 # DNS server (required for resolution)
    "1.1.1.1",                 # Cloudflare DNS
    "registry.npmjs.org",      # Test domain
]
refresh_interval_minutes = 30
""")
        config_file = f.name

    try:
        # Start container in background with allowlist mode
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

        # Extract container name from output (check both stdout and stderr)
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, f"Could not find container name in output: {output}"

        # Fix DNS in container (required for resolution)
        subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "echo 'nameserver 8.8.8.8' > /etc/resolv.conf",
            ],
            capture_output=True,
            timeout=10,
        )

        # Test: curl allowed domain (should work)
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "curl",
                "-I",
                "-m",
                "10",
                "https://registry.npmjs.org",
            ],
            capture_output=True,
            text=True,
            timeout=15,
        )

        assert result.returncode == 0, f"Failed to reach allowed domain: {result.stderr}"
        assert "HTTP" in result.stderr, f"No HTTP response from allowed domain: {result.stderr}"

    finally:
        os.unlink(config_file)


def test_allowlist_blocks_non_allowed_domains(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that allowlist mode blocks domains NOT in allowed_domains.

    Verifies that containers cannot reach domains not explicitly listed.
    """
    # Create temporary config with minimal allowlist
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",                 # DNS server only
    "1.1.1.1",
    "registry.npmjs.org",      # Only this domain allowed
]
refresh_interval_minutes = 30
""")
        config_file = f.name

    try:
        # Start container in background
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

        # Extract container name (check both stdout and stderr)
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, "Could not find container name in output"

        # Fix DNS
        subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "echo 'nameserver 8.8.8.8' > /etc/resolv.conf",
            ],
            capture_output=True,
            timeout=10,
        )

        # Wait for firewall rules to be fully applied (CI timing issue)
        time.sleep(2)

        # Test: curl blocked domain (should fail)
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "timeout",
                "5",
                "curl",
                "-I",
                "-m",
                "5",
                "https://github.com",
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )

        # Should fail to connect (either REJECT with "Connection refused" or timeout)
        assert result.returncode != 0, (
            f"Should not reach blocked domain github.com: {result.stderr}"
        )
        # Accept either explicit rejection or timeout (exit code 124) as valid blocking
        is_rejected = "Connection refused" in result.stderr or "Failed to connect" in result.stderr
        is_timeout = "exit status 124" in result.stderr or result.returncode == 124
        assert is_rejected or is_timeout, (
            f"Expected connection failure for blocked domain: {result.stderr}"
        )

        # Test: curl another blocked domain
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "timeout",
                "5",
                "curl",
                "-I",
                "-m",
                "5",
                "http://example.com",
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )

        assert result.returncode != 0, (
            f"Should not reach blocked domain example.com: {result.stderr}"
        )

    finally:
        os.unlink(config_file)


def test_allowlist_always_blocks_rfc1918(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that allowlist mode always blocks RFC1918 private networks.

    Even with domains in the allowlist, RFC1918 addresses should be blocked.
    """
    # Create temporary config
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",
    "1.1.1.1",
]
refresh_interval_minutes = 30
""")
        config_file = f.name

    try:
        # Start container
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

        # Extract container name (check both stdout and stderr)
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, "Could not find container name"

        # Test: RFC1918 10.0.0.0/8 (should be blocked)
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "timeout",
                "3",
                "curl",
                "-I",
                "-m",
                "3",
                "http://10.0.0.1",
            ],
            capture_output=True,
            text=True,
            timeout=5,
        )

        assert result.returncode != 0, f"Should block RFC1918 10.0.0.1: {result.stderr}"

        # Test: RFC1918 192.168.0.0/16 (should be blocked)
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "timeout",
                "3",
                "curl",
                "-I",
                "-m",
                "3",
                "http://192.168.1.1",
            ],
            capture_output=True,
            text=True,
            timeout=5,
        )

        assert result.returncode != 0, f"Should block RFC1918 192.168.1.1: {result.stderr}"

        # Test: RFC1918 172.16.0.0/12 (should be blocked)
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "timeout",
                "3",
                "curl",
                "-I",
                "-m",
                "3",
                "http://172.16.0.1",
            ],
            capture_output=True,
            text=True,
            timeout=5,
        )

        assert result.returncode != 0, f"Should block RFC1918 172.16.0.1: {result.stderr}"

    finally:
        os.unlink(config_file)


def test_allowlist_blocks_public_ips_not_in_list(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that allowlist mode blocks public IPs not in the allowlist.

    Verifies that OVN's implicit default-deny blocks non-allowed public IPs.
    """
    # Create temporary config with only DNS
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",    # Only DNS allowed
    "1.1.1.1",
]
refresh_interval_minutes = 30
""")
        config_file = f.name

    try:
        # Start container
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

        # Extract container name (check both stdout and stderr)
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, "Could not find container name"

        # Test: Random public IP not in allowlist (should be blocked by implicit default-deny)
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "timeout",
                "3",
                "curl",
                "-I",
                "-m",
                "3",
                "http://9.9.9.9",
            ],
            capture_output=True,
            text=True,
            timeout=5,
        )

        assert result.returncode != 0, (
            f"Should block non-allowed public IP 9.9.9.9: {result.stderr}"
        )
        # Accept either explicit rejection or timeout (exit code 124) as valid blocking
        is_rejected = "Connection refused" in result.stderr or "Failed to connect" in result.stderr
        is_timeout = "exit status 124" in result.stderr or result.returncode == 124
        assert is_rejected or is_timeout, (
            f"Expected connection failure for non-allowed IP: {result.stderr}"
        )

    finally:
        os.unlink(config_file)


def test_allowlist_allows_host_to_access_container_services(
    coi_binary, workspace_dir, cleanup_containers
):
    """
    Test that host can access services running in container (established connections).

    When a service like Puma or HTTP server runs in the container, the host should
    be able to access it because established/related connections back to the host
    are allowed via connection tracking.
    """
    # Create temporary config with allowlist
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write(
            """
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",
    "1.1.1.1",
]
refresh_interval_minutes = 30
"""
        )
        config_file = f.name

    try:
        # Start container in background
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

        # Extract container name (check both stdout and stderr)
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, "Could not find container name"

        # Start a simple HTTP server in the container on port 8000
        subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "nohup python3 -m http.server 8000 > /tmp/http-server.log 2>&1 &",
            ],
            capture_output=True,
            timeout=10,
        )

        # Give server time to start
        import time

        time.sleep(2)

        # Get container's IP address using incus list (more reliable than hostname -I)
        result = subprocess.run(
            ["incus", "list", container_name, "--format=json"],
            capture_output=True,
            text=True,
            timeout=10,
        )

        assert result.returncode == 0, f"Failed to get container info: {result.stderr}"

        container_info = json.loads(result.stdout)[0]
        # Get IPv4 address from eth0 interface
        container_ip = container_info["state"]["network"]["eth0"]["addresses"][0]["address"]

        # Test: Host should be able to access the HTTP server
        # This verifies established connection tracking works
        result = subprocess.run(
            ["curl", "-I", "-m", "5", f"http://{container_ip}:8000"],
            capture_output=True,
            text=True,
            timeout=10,
        )

        assert result.returncode == 0, (
            f"Host should be able to access container service: {result.stderr}"
        )
        assert "HTTP" in result.stdout, f"Expected HTTP response from container: {result.stdout}"

    finally:
        os.unlink(config_file)


def test_restricted_allows_host_to_access_container_services(
    coi_binary, workspace_dir, cleanup_containers
):
    """
    Test that host can access services in restricted mode too.

    Verifies that the established connection rule works in both allowlist
    and restricted network modes.
    """
    # Start container in background with restricted mode
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
    )

    assert result.returncode == 0, f"Failed to start container: {result.stderr}"

    # Extract container name (check both stdout and stderr)
    container_name = None
    output = result.stdout + result.stderr
    for line in output.split("\n"):
        if "Container: " in line:
            container_name = line.split("Container: ")[1].strip()
            break

    assert container_name, "Could not find container name"

    # Start a simple HTTP server in the container on port 8000
    subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            "nohup python3 -m http.server 8000 > /tmp/http-server.log 2>&1 &",
        ],
        capture_output=True,
        timeout=10,
    )

    # Give server time to start
    import time

    time.sleep(2)

    # Get container's IP address using incus list (more reliable than hostname -I)
    result = subprocess.run(
        ["incus", "list", container_name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Failed to get container info: {result.stderr}"

    container_info = json.loads(result.stdout)[0]
    # Get IPv4 address from eth0 interface
    container_ip = container_info["state"]["network"]["eth0"]["addresses"][0]["address"]

    # Test: Host should be able to access the HTTP server
    result = subprocess.run(
        ["curl", "-I", "-m", "5", f"http://{container_ip}:8000"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, (
        f"Host should be able to access container service: {result.stderr}"
    )
    assert "HTTP" in result.stdout, f"Expected HTTP response from container: {result.stdout}"
