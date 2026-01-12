"""
Test for coi image publish - running container.

Tests that:
1. Launch a container (running state)
2. Try to publish running container
3. Verify behavior (may fail or auto-stop)
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_publish_running_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test publishing a running container.

    Flow:
    1. Launch a container
    2. Try to publish while running
    3. Verify appropriate behavior
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    test_image_name = f"test-running-{container_name}"

    # === Phase 1: Launch container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 2: Try to publish running container ===

    result = subprocess.run(
        [coi_binary, "image", "publish", container_name, test_image_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Behavior depends on implementation - may fail or auto-stop
    # We just verify it handles the situation (either way is acceptable)
    combined_output = result.stdout + result.stderr

    if result.returncode == 0:
        # If it succeeded, clean up the image
        subprocess.run(
            [coi_binary, "image", "delete", test_image_name],
            capture_output=True,
            timeout=60,
        )
    else:
        # If it failed, should show appropriate message
        assert (
            "running" in combined_output.lower()
            or "stop" in combined_output.lower()
            or "failed" in combined_output.lower()
        ), f"Should indicate issue with running container. Got:\n{combined_output}"

    # === Phase 3: Cleanup container ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
