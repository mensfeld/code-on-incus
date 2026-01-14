"""
Test for network isolation - restricted mode blocks private networks.

Tests that:
1. Container in restricted mode (default) cannot reach RFC1918 addresses
2. Blocks 10.0.0.0/8
3. Blocks 172.16.0.0/12
4. Blocks 192.168.0.0/16
"""

import subprocess
import time


def test_restricted_blocks_private_networks(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that restricted mode blocks access to private networks.

    Flow:
    1. Start shell in background (default: restricted mode)
    2. Try to curl private network addresses
    3. Verify connections are blocked/rejected
    4. Cleanup container
    """
    # Start shell in background
    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background", "--debug"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start successfully. stderr: {result.stderr}"

    # Give container time to fully start
    time.sleep(5)

    # Test 1: Try to curl 10.0.0.1 (should be blocked)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "curl -s --connect-timeout 2 http://10.0.0.1",
        ],
        capture_output=True,
        text=True,
        timeout=10,
    )

    # Should fail (non-zero exit code) or timeout
    assert result.returncode != 0, "Should not be able to reach 10.0.0.1 (RFC1918 range)"

    # Test 2: Try to curl 192.168.1.1 (should be blocked)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "curl -s --connect-timeout 2 http://192.168.1.1",
        ],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode != 0, "Should not be able to reach 192.168.1.1 (RFC1918 range)"

    # Test 3: Try to curl 172.16.0.1 (should be blocked)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "curl -s --connect-timeout 2 http://172.16.0.1",
        ],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode != 0, "Should not be able to reach 172.16.0.1 (RFC1918 range)"
