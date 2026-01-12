"""
Test for coi list - shows multiple containers.

Tests that:
1. Launch multiple containers
2. Run coi list
3. Verify all containers appear
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_list_multiple_containers(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list shows multiple containers.

    Flow:
    1. Launch 2 containers
    2. Run coi list
    3. Verify both containers appear
    4. Cleanup
    """
    container1 = calculate_container_name(workspace_dir, 1)
    container2 = calculate_container_name(workspace_dir, 2)

    # === Phase 1: Launch containers ===

    for container_name in [container1, container2]:
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

    # === Phase 3: Verify both containers appear ===

    assert container1 in output, f"Container {container1} should appear in list. Got:\n{output}"

    assert container2 in output, f"Container {container2} should appear in list. Got:\n{output}"

    # === Phase 4: Cleanup ===

    for container_name in [container1, container2]:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
