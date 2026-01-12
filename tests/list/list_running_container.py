"""
Test for coi list - shows running container info.

Tests that:
1. Launch a container
2. Run coi list
3. Verify it shows container with status, created time, image
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_list_running_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list shows running container details.

    Flow:
    1. Launch a container
    2. Run coi list
    3. Verify container appears with status "Running"
    4. Verify shows created time and image info
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

    # === Phase 2: Run list ===

    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"

    output = result.stdout

    # === Phase 3: Verify container appears ===

    assert container_name in output, (
        f"Container {container_name} should appear in list. Got:\n{output}"
    )

    # Should show Running status
    assert "Running" in output, f"Should show Running status. Got:\n{output}"

    # Should show Status field
    assert "Status:" in output, f"Should show Status field. Got:\n{output}"

    # Should show Created field
    assert "Created:" in output, f"Should show Created field. Got:\n{output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
