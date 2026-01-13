"""
Test for coi container exec - poweroff without password.

Tests that:
1. Launch a container
2. Execute sudo poweroff as the code user (no password required)
3. Verify command succeeds (exit code 0)
4. Verify container stops cleanly
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_exec_poweroff_without_sudo(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that the code user can run sudo poweroff without a password.

    Flow:
    1. Launch a container
    2. Execute sudo poweroff as code user (no password required)
    3. Verify command succeeds
    4. Verify container stops
    5. Cleanup
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

    # === Phase 2: Execute sudo poweroff (no password required) ===

    # Execute sudo poweroff as the code user (uid 1000) - no password required
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--user",
            "1000",
            "--",
            "sudo",
            "poweroff",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # sudo poweroff should succeed (exit code 0) without requiring a password
    assert result.returncode == 0, (
        f"sudo poweroff should succeed without password. stderr: {result.stderr}"
    )

    # === Phase 3: Wait for container to stop ===

    # Give container time to shutdown gracefully
    time.sleep(5)

    # === Phase 4: Verify container stopped ===

    # Check if container is still running
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # running command should return non-zero if container is not running
    assert result.returncode != 0, (
        "Container should be stopped after poweroff. "
        f"Exit code: {result.returncode}, Output: {result.stdout + result.stderr}"
    )

    # === Phase 5: Cleanup ===

    # Clean up the stopped container
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
