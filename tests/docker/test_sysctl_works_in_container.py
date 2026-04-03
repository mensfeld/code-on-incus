"""
Test for sysctl support - verifies sysctl writes work inside containers.

Tests that:
1. Launch a container
2. Write a sysctl value (net.ipv4.ip_unprivileged_port_start=0)
3. Verify the write succeeds without "permission denied"
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_sysctl_works_in_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that sysctl writes work inside the container.

    Some Docker images (e.g. Kafka) need to tune kernel parameters via sysctl.
    This requires linux.sysctl.net.ipv4.ip_unprivileged_port_start=0 on the container.

    Flow:
    1. Launch a container
    2. Run sysctl -w net.ipv4.ip_unprivileged_port_start=0
    3. Verify the command succeeds
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 2: Write a sysctl value ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "sysctl",
            "-w",
            "net.ipv4.ip_unprivileged_port_start=0",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"sysctl write should succeed with ip_unprivileged_port_start pre-set. stderr: {result.stderr}"
    )
    output = (result.stdout + result.stderr).lower()
    assert "permission denied" not in output, f"sysctl write should not be denied. output: {output}"

    # === Phase 3: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
