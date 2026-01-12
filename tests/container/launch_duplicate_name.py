"""
Test for coi container launch - fails with duplicate container name.

Tests that:
1. Launch a container
2. Try to launch another container with same name
3. Verify second launch fails
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    get_container_list,
)


def test_launch_duplicate_name(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that launching container with duplicate name fails.

    Flow:
    1. Launch a container
    2. Try to launch another with same name
    3. Verify second launch fails
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch first container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"First container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # Verify container exists
    containers = get_container_list()
    assert container_name in containers, f"Container {container_name} should exist"

    # === Phase 2: Try to launch duplicate ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )

    # === Phase 3: Verify failure ===

    assert result.returncode != 0, "Launching container with duplicate name should fail"

    combined_output = result.stdout + result.stderr
    has_error = (
        "exist" in combined_output.lower()
        or "already" in combined_output.lower()
        or "duplicate" in combined_output.lower()
        or "error" in combined_output.lower()
    )

    assert has_error, f"Should indicate duplicate/existing container. Got:\n{combined_output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
