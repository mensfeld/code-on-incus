"""
Test for coi image publish and delete - full lifecycle.

Tests that:
1. Launch and stop a container
2. Publish container as image
3. Verify image exists
4. Delete the image
5. Verify image no longer exists
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_publish_and_delete_image(coi_binary, cleanup_containers, workspace_dir):
    """
    Test publishing a container as image and deleting it.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Publish as image with unique name
    4. Verify image exists
    5. Delete the image
    6. Verify image no longer exists
    7. Cleanup container
    """
    container_name = calculate_container_name(workspace_dir, 1)
    test_image_name = f"test-publish-{container_name}"

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

    # === Phase 3: Publish as image ===

    result = subprocess.run(
        [
            coi_binary,
            "image",
            "publish",
            container_name,
            test_image_name,
            "--description",
            "Test image for integration tests",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Image publish should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "fingerprint" in combined_output.lower() or test_image_name in combined_output, (
        f"Should show fingerprint or alias. Got:\n{combined_output}"
    )

    # === Phase 4: Verify image exists ===

    result = subprocess.run(
        [coi_binary, "image", "exists", test_image_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Published image should exist. stderr: {result.stderr}"

    # === Phase 5: Delete the image ===

    result = subprocess.run(
        [coi_binary, "image", "delete", test_image_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Image delete should succeed. stderr: {result.stderr}"

    # === Phase 6: Verify image no longer exists ===

    result = subprocess.run(
        [coi_binary, "image", "exists", test_image_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, f"Deleted image should not exist. stdout: {result.stdout}"

    # === Phase 7: Cleanup container ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
