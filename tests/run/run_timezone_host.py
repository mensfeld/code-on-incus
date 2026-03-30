"""
Test for coi run - host timezone inheritance.

Tests that:
1. Run with default timezone mode (host)
2. Container's timezone matches the host's timezone
"""

import subprocess


def test_run_timezone_host(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that the container inherits the host timezone by default.

    Flow:
    1. Detect the host timezone abbreviation via 'date +%Z'
    2. Run coi run (default --timezone=host) and capture 'date +%Z' from container
    3. Verify both match
    """
    # Get host timezone abbreviation
    host_result = subprocess.run(
        ["date", "+%Z"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert host_result.returncode == 0, f"Host date failed: {host_result.stderr}"
    host_tz = host_result.stdout.strip()
    assert host_tz, "Could not detect host timezone"

    # Run in container with default (host) timezone
    container_result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "date",
            "+%Z",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert container_result.returncode == 0, (
        f"Run should succeed. stderr: {container_result.stderr}"
    )

    container_tz = container_result.stdout.strip()
    assert container_tz == host_tz, (
        f"Container timezone '{container_tz}' should match host timezone '{host_tz}'"
    )
