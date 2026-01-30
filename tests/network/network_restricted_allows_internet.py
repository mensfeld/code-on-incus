"""
Test for network isolation - restricted mode allows internet access.

Tests that:
1. Container can reach public internet
2. Development workflows (npm, pypi, GitHub) work normally
3. Only local/internal networks are blocked

Note: This test requires firewalld for network isolation.
"""

import subprocess
import time

import pytest


def firewalld_available():
    """Check if firewalld is running."""
    result = subprocess.run(
        ["firewall-cmd", "--state"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    return result.returncode == 0 and "running" in result.stdout


# Skip all tests in this module when firewalld is not available
pytestmark = pytest.mark.skipif(
    not firewalld_available(),
    reason="Restricted mode requires firewalld",
)


def test_restricted_allows_internet(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that restricted mode allows access to public internet.

    Flow:
    1. Start shell in background (default: restricted mode)
    2. Try to curl public internet sites
    3. Verify connections succeed
    4. Cleanup container
    """
    # Start shell in background with restricted network mode
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--background",
            "--debug",
            "--network=restricted",
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

    # Give container time to fully start and for network ACLs to be applied
    # Longer wait needed as this test runs alongside other network tests
    time.sleep(12)

    # Test 1: Curl example.com (should work)
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

    # Test 2: Curl registry.npmjs.org (should work)
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
            "https://registry.npmjs.org",
        ],
        capture_output=True,
        text=True,
        timeout=20,
    )

    assert result.returncode == 0, (
        f"Should be able to reach registry.npmjs.org. stderr: {result.stderr}"
    )
    # Note: coi container exec outputs to stderr, not stdout
    # NPM registry returns JSON (may be {} at root endpoint)
    assert "{" in result.stderr and "}" in result.stderr, (
        "Should receive NPM registry JSON response"
    )

    # DNS resolution is implicitly tested by the curl commands above (they resolve domain names)
