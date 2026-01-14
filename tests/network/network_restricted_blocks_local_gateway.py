"""
Test for network isolation - restricted mode blocks local gateway.

Tests that:
1. Container in restricted mode (default) cannot reach local gateway
2. Works regardless of what private network range the host uses
3. Dynamically discovers the gateway IP
"""

import subprocess
import time


def test_restricted_blocks_local_gateway(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that restricted mode blocks access to local network gateway.

    This test discovers the gateway IP dynamically, so it works on any
    private network range (10.x.x.x, 172.16-31.x.x, 192.168.x.x).

    Flow:
    1. Start shell in background (default: restricted mode)
    2. Extract container name from output
    3. Discover the gateway IP from inside container
    4. Try to connect to gateway
    5. Verify connection is blocked by ACL
    6. Cleanup container
    """
    # Start shell in background (default restricted mode)
    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background", "--debug"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start successfully. stderr: {result.stderr}"

    # Should see "restricted" or "blocking" in the output
    assert "restricted" in result.stderr.lower() or "blocking" in result.stderr.lower(), (
        f"Should indicate restricted network mode. stderr: {result.stderr}"
    )

    # Extract container name from output
    # Look for pattern like "Container name: coi-xxxxx-1"
    container_name = None
    for line in result.stderr.split("\n"):
        if "Container name:" in line:
            container_name = line.split("Container name:")[-1].strip()
            break

    assert container_name is not None, (
        f"Should find container name in output. stderr: {result.stderr}"
    )

    # Give container time to fully start
    time.sleep(5)

    # Discover the gateway IP from inside the container
    # Using 'ip route show default' to find the gateway
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ip", "route", "show", "default"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Should be able to run ip route. stderr: {result.stderr}"

    # Parse gateway IP from output like: "default via 10.0.3.1 dev eth0 ..."
    # Note: coi container exec outputs to stderr, not stdout
    output = result.stderr.strip()
    gateway_ip = None

    if "default via" in output:
        parts = output.split()
        try:
            via_index = parts.index("via")
            if via_index + 1 < len(parts):
                gateway_ip = parts[via_index + 1]
        except (ValueError, IndexError):
            pass

    assert gateway_ip is not None, f"Should be able to discover gateway IP. Got: {output}"

    # Verify gateway is in RFC1918 range (should be for Incus containers)
    is_private = (
        gateway_ip.startswith("10.")
        or gateway_ip.startswith("192.168.")
        or any(gateway_ip.startswith(f"172.{i}.") for i in range(16, 32))
    )
    assert is_private, f"Gateway {gateway_ip} should be in RFC1918 private range"

    # Try to connect to the gateway (should be blocked)
    # Use a quick timeout since ACL should reject immediately
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "curl",
            "-s",
            "--connect-timeout",
            "2",
            f"http://{gateway_ip}",
        ],
        capture_output=True,
        text=True,
        timeout=10,
    )

    # Should fail (non-zero exit code) because ACL blocks it
    assert result.returncode != 0, (
        f"Should not be able to reach gateway {gateway_ip} (RFC1918 blocked)"
    )

    # The error message might indicate connection refused/timeout/network unreachable
    # All of these are fine - just shouldn't succeed
