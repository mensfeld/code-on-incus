"""
Test for coi list - shows stopped container.

Tests that:
1. Launch a container
2. Stop it
3. Run coi list
4. Verify it shows container with "Stopped" status
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_list_stopped_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list shows stopped containers.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Run coi list
    4. Verify container appears with "Stopped" status
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

    # === Phase 2: Stop container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 3: Run list ===

    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"

    output = result.stdout

    # === Phase 4: Verify container shows as stopped ===

    assert container_name in output, (
        f"Container {container_name} should appear in list. Got:\n{output}"
    )

    assert "Stopped" in output, f"Should show Stopped status. Got:\n{output}"

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
