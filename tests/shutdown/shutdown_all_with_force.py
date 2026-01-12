"""
Test for coi shutdown --all --force.

Tests that:
1. Launch multiple containers
2. Run shutdown --all --force
3. Verify all containers are deleted
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_shutdown_all_with_force(coi_binary, cleanup_containers, workspace_dir):
    """
    Test shutting down all containers with --all --force.

    Flow:
    1. Launch two containers
    2. Run coi shutdown --all --force
    3. Verify all containers are deleted
    """
    container1 = calculate_container_name(workspace_dir, 1)
    container2 = calculate_container_name(workspace_dir, 2)

    # Launch first container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container1],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch container 1 should succeed. stderr: {result.stderr}"

    # Launch second container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container2],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch container 2 should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # Shutdown all with force
    result = subprocess.run(
        [coi_binary, "shutdown", "--all", "--force"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Shutdown --all --force should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "shutdown" in combined_output.lower(), (
        f"Should show shutdown message. Got:\n{combined_output}"
    )

    # Verify containers no longer exist
    time.sleep(2)

    result = subprocess.run(
        [coi_binary, "container", "exists", container1],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Container 1 should not exist after shutdown --all"

    result = subprocess.run(
        [coi_binary, "container", "exists", container2],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Container 2 should not exist after shutdown --all"
