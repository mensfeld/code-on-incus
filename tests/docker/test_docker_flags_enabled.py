"""
Test for Docker support flags - verifies nesting flags are automatically enabled.

Tests that:
1. Launch a container
2. Verify security.nesting is set to true
3. Verify security.syscalls.intercept.mknod is set to true
4. Verify security.syscalls.intercept.setxattr is set to true
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_docker_flags_enabled(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that Docker support flags are automatically enabled on container launch.

    Flow:
    1. Launch a container
    2. Verify all three Docker-related security flags are enabled
    3. Cleanup
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

    # === Phase 2: Verify Docker support flags are enabled ===

    # Check security.nesting
    result = subprocess.run(
        ["incus", "--project", "default", "config", "get", container_name, "security.nesting"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Failed to get security.nesting config. stderr: {result.stderr}"
    assert result.stdout.strip() == "true", "security.nesting should be enabled for Docker support"

    # Check security.syscalls.intercept.mknod
    result = subprocess.run(
        [
            "incus",
            "--project",
            "default",
            "config",
            "get",
            container_name,
            "security.syscalls.intercept.mknod",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Failed to get security.syscalls.intercept.mknod config. stderr: {result.stderr}"
    )
    assert result.stdout.strip() == "true", (
        "security.syscalls.intercept.mknod should be enabled for Docker support"
    )

    # Check security.syscalls.intercept.setxattr
    result = subprocess.run(
        [
            "incus",
            "--project",
            "default",
            "config",
            "get",
            container_name,
            "security.syscalls.intercept.setxattr",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Failed to get security.syscalls.intercept.setxattr config. stderr: {result.stderr}"
    )
    assert result.stdout.strip() == "true", (
        "security.syscalls.intercept.setxattr should be enabled for Docker support"
    )

    # === Phase 3: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
