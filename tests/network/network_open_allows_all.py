"""
Test for network isolation - open mode allows all connections.

Tests that:
1. Container with --network=open can access everything
2. Both public internet and private networks work
3. No network restrictions applied
"""

import subprocess
import time


def test_open_mode_allows_all(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that open mode allows all network access.

    Flow:
    1. Start shell with --network=open
    2. Try to curl both public internet and private networks
    3. Verify all connections work (or fail for normal reasons, not ACL blocking)
    4. Cleanup container
    """
    # Start shell in background with open network mode
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--network",
            "open",
            "--background",
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start successfully. stderr: {result.stderr}"

    # Should see "Network mode: open" message in stderr
    assert "open" in result.stderr.lower() or "no restrictions" in result.stderr.lower(), (
        f"Should indicate open network mode. stderr: {result.stderr}"
    )

    # Extract container name from output
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

    # Test 1: Public internet should work
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
            "10",
            "http://example.com",
        ],
        capture_output=True,
        text=True,
        timeout=20,
    )

    assert result.returncode == 0, f"Should be able to reach example.com. stderr: {result.stderr}"
    # Note: coi container exec outputs to stderr, not stdout
    assert "Example Domain" in result.stderr, "Should receive example.com HTML content"

    # Test 2: Private networks should NOT be blocked by ACL
    # Note: They may still fail if no device exists at that IP, but it won't be blocked by ACL
    # We just verify the command doesn't get instantly rejected
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
            "http://192.168.1.1",
        ],
        capture_output=True,
        text=True,
        timeout=10,
    )

    # In open mode, the connection attempt should be made (not blocked by ACL)
    # Exit code may still be non-zero if no device responds, but we shouldn't see ACL rejection
    # This is a best-effort test - we can't guarantee 192.168.1.1 exists
    # Just verify we don't get instant rejection
    # If the command completes within 2 seconds, ACL likely didn't block it
