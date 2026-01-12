"""
Test for coi list - shows image description.

Tests that:
1. Launch a container from coi image
2. Run coi list
3. Verify it shows the image description
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_list_shows_image(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list shows image description.

    Flow:
    1. Launch a container from coi image
    2. Run coi list
    3. Verify Image field appears
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

    # === Phase 3: Verify image info ===

    assert container_name in output, f"Container should appear. Got:\n{output}"

    # Should show Image field (coi image has a description)
    assert "Image:" in output, f"Should show Image field. Got:\n{output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
