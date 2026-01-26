"""
Test for coi list - shows IPv4 address for running containers.

Tests that:
1. Launch a container
2. Run coi list
3. Verify it shows the container's IPv4 address
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_list_shows_ipv4_running(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list shows IPv4 address for running containers.

    Flow:
    1. Launch a container
    2. Run coi list
    3. Verify container appears with IPv4 address
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 2: Run list ===

    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"

    output = result.stdout

    # === Phase 3: Verify IPv4 appears ===

    assert container_name in output, (
        f"Container {container_name} should appear in list. Got:\n{output}"
    )

    # Should show IPv4 field
    assert "IPv4:" in output, f"Should show IPv4 field for running container. Got:\n{output}"

    # Should show an IP address (pattern: X.X.X.X)
    # We don't check the exact IP, just that one is shown
    lines = output.split("\n")
    found_ipv4 = False
    for line in lines:
        if "IPv4:" in line:
            # Check if there's something after "IPv4:" that looks like an IP
            parts = line.split("IPv4:")
            if len(parts) > 1 and parts[1].strip():
                found_ipv4 = True
                break

    assert found_ipv4, f"Should show an IPv4 address for running container. Got:\n{output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
