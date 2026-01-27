"""
Integration tests for network allowlist mode.

Tests the domain allowlisting feature with DNS resolution and IP-based filtering.

Note: These tests require OVN networking.
"""

import os
import subprocess
import tempfile

# OVN networking is now configured in CI, so these tests can run!


def test_allowlist_mode_allows_specified_domains(
    coi_binary, workspace_dir, cleanup_containers
):
    """
    Test that allowlist mode allows access to domains in allowed_domains.

    Verifies that containers can reach domains explicitly listed in the allowlist.
    """
    # Create temporary config with allowlist
    with tempfile.NamedTemporaryFile(mode='w', suffix='.toml', delete=False) as f:
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
        env['COI_CONFIG'] = config_file

        result = subprocess.run(
            [coi_binary, "shell", "--workspace", workspace_dir, "--network=allowlist", "--background"],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

        # Extract container name from output (check both stdout and stderr)
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split('\n'):
            if 'Container: ' in line:
                container_name = line.split('Container: ')[1].strip()
                break

        assert container_name, f"Could not find container name in output: {output}"

        # Fix DNS in container (required for resolution)
        subprocess.run(
            [coi_binary, "container", "exec", container_name, "--",
             "bash", "-c", "echo 'nameserver 8.8.8.8' > /etc/resolv.conf"],
            capture_output=True,
            timeout=10,
        )

        # Test: curl allowed domain (should work)
        result = subprocess.run(
            [coi_binary, "container", "exec", container_name,
             "--", "curl", "-I", "-m", "10", "https://registry.npmjs.org"],
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
    with tempfile.NamedTemporaryFile(mode='w', suffix='.toml', delete=False) as f:
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
        env['COI_CONFIG'] = config_file

        result = subprocess.run(
            [coi_binary, "shell", "--workspace", workspace_dir, "--network=allowlist", "--background"],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

        # Extract container name (check both stdout and stderr)
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split('\n'):
            if 'Container: ' in line:
                container_name = line.split('Container: ')[1].strip()
                break

        assert container_name, "Could not find container name in output"

        # Fix DNS
        subprocess.run(
            [coi_binary, "container", "exec", container_name, "--",
             "bash", "-c", "echo 'nameserver 8.8.8.8' > /etc/resolv.conf"],
            capture_output=True,
            timeout=10,
        )

        # Test: curl blocked domain (should fail)
        result = subprocess.run(
            [coi_binary, "container", "exec", container_name,
             "--", "timeout", "5", "curl", "-I", "-m", "5", "https://github.com"],
            capture_output=True,
            text=True,
            timeout=10,
        )

        # Should fail to connect
        assert result.returncode != 0, f"Should not reach blocked domain github.com: {result.stderr}"
        assert "Connection refused" in result.stderr or "Failed to connect" in result.stderr, \
            f"Expected connection failure for blocked domain: {result.stderr}"

        # Test: curl another blocked domain
        result = subprocess.run(
            [coi_binary, "container", "exec", container_name,
             "--", "timeout", "5", "curl", "-I", "-m", "5", "http://example.com"],
            capture_output=True,
            text=True,
            timeout=10,
        )

        assert result.returncode != 0, f"Should not reach blocked domain example.com: {result.stderr}"

    finally:
        os.unlink(config_file)


def test_allowlist_always_blocks_rfc1918(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that allowlist mode always blocks RFC1918 private networks.

    Even with domains in the allowlist, RFC1918 addresses should be blocked.
    """
    # Create temporary config
    with tempfile.NamedTemporaryFile(mode='w', suffix='.toml', delete=False) as f:
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
        env['COI_CONFIG'] = config_file

        result = subprocess.run(
            [coi_binary, "shell", "--workspace", workspace_dir, "--network=allowlist", "--background"],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

        # Extract container name (check both stdout and stderr)
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split('\n'):
            if 'Container: ' in line:
                container_name = line.split('Container: ')[1].strip()
                break

        assert container_name, "Could not find container name"

        # Test: RFC1918 10.0.0.0/8 (should be blocked)
        result = subprocess.run(
            [coi_binary, "container", "exec", container_name,
             "--", "timeout", "3", "curl", "-I", "-m", "3", "http://10.0.0.1"],
            capture_output=True,
            text=True,
            timeout=5,
        )

        assert result.returncode != 0, f"Should block RFC1918 10.0.0.1: {result.stderr}"

        # Test: RFC1918 192.168.0.0/16 (should be blocked)
        result = subprocess.run(
            [coi_binary, "container", "exec", container_name,
             "--", "timeout", "3", "curl", "-I", "-m", "3", "http://192.168.1.1"],
            capture_output=True,
            text=True,
            timeout=5,
        )

        assert result.returncode != 0, f"Should block RFC1918 192.168.1.1: {result.stderr}"

        # Test: RFC1918 172.16.0.0/12 (should be blocked)
        result = subprocess.run(
            [coi_binary, "container", "exec", container_name,
             "--", "timeout", "3", "curl", "-I", "-m", "3", "http://172.16.0.1"],
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
    with tempfile.NamedTemporaryFile(mode='w', suffix='.toml', delete=False) as f:
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
        env['COI_CONFIG'] = config_file

        result = subprocess.run(
            [coi_binary, "shell", "--workspace", workspace_dir, "--network=allowlist", "--background"],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

        # Extract container name (check both stdout and stderr)
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split('\n'):
            if 'Container: ' in line:
                container_name = line.split('Container: ')[1].strip()
                break

        assert container_name, "Could not find container name"

        # Test: Random public IP not in allowlist (should be blocked by implicit default-deny)
        result = subprocess.run(
            [coi_binary, "container", "exec", container_name,
             "--", "timeout", "3", "curl", "-I", "-m", "3", "http://9.9.9.9"],
            capture_output=True,
            text=True,
            timeout=5,
        )

        assert result.returncode != 0, f"Should block non-allowed public IP 9.9.9.9: {result.stderr}"
        assert "Connection refused" in result.stderr or "Failed to connect" in result.stderr, \
            f"Expected connection failure for non-allowed IP: {result.stderr}"

    finally:
        os.unlink(config_file)
