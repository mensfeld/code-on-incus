"""
Test for coi attach - attach to stopped container fails gracefully.

Tests that:
1. Launch a container
2. Stop it
3. Try to attach
4. Verify it fails with appropriate error
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_attach_stopped_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi attach to a stopped container fails gracefully.

    Flow:
    1. Launch a container directly
    2. Stop it (but don't delete)
    3. Try to attach
    4. Verify it fails with 'not running' error
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

    assert result.returncode == 0, \
        f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # Verify container is running
    containers = get_container_list()
    assert container_name in containers, \
        f"Container {container_name} should be running"

    # === Phase 2: Stop container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, \
        f"Container stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 3: Try to attach to stopped container ===

    result = subprocess.run(
        [coi_binary, "attach", container_name],
        capture_output=True,
        text=True,
        timeout=10,
    )

    # Should fail
    combined_output = result.stdout + result.stderr
    
    # Either returns error or container is not found in running list
    attach_failed = (
        result.returncode != 0 or
        "not found" in combined_output.lower() or
        "not running" in combined_output.lower() or
        "No active" in combined_output
    )

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    time.sleep(1)
    containers = get_container_list()
    assert container_name not in containers, \
        f"Container {container_name} should be deleted"

    # Assert attach failed appropriately
    assert attach_failed, \
        f"Attach to stopped container should fail. Got:\n{combined_output}"
