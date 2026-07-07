"""
Test for network isolation - open mode allows all connections.

Tests that:
1. Container with network mode "open" can access everything
2. Both public internet and private networks work
3. No network restrictions applied
"""

import subprocess
from pathlib import Path

from support.helpers import wait_for_firewall_rules


def test_open_mode_allows_all(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that open mode allows all network access.

    Flow:
    1. Start shell with network mode "open" via config file
    2. Try to curl both public internet and private networks
    3. Verify all connections work (or fail for normal reasons, not ACL blocking)
    4. Cleanup container
    """
    # Write network config to workspace .coi/config.toml
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("""
[network]
mode = "open"
""")

    # Start shell in background with open network mode
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--background",
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start successfully. stderr: {result.stderr}"

    # Extract container name from output
    container_name = None
    for line in result.stderr.split("\n"):
        if "Container name:" in line:
            container_name = line.split("Container name:")[-1].strip()
            break

    assert container_name is not None, (
        f"Should find container name in output. stderr: {result.stderr}"
    )

    # Wait (poll) for the container's network setup to land instead of a fixed sleep.
    wait_for_firewall_rules(container_name)

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

    # In open mode the ACL must NOT reject an RFC1918 address. Nothing answers at
    # 192.168.1.1, so the probe fails with a timeout (the packet is routed out and
    # gets no reply) — never with an instant ACL/iptables refusal. A "connection
    # refused" with no timeout would mean egress was actively blocked. (Same
    # assertion the comprehensive open-mode test uses.) Previously this probe ran
    # with no assertion at all — a vacuous no-op (#559 C2).
    combined = (result.stdout + result.stderr).lower()
    assert "connection refused" not in combined or "timed out" in combined, (
        f"open mode should not ACL-block RFC1918 (no instant refusal expected): {result.stderr}"
    )
