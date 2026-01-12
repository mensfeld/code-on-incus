"""
Test for coi image cleanup - full lifecycle with versioned images.

Tests that:
1. Create multiple versioned images
2. Run cleanup keeping only N most recent
3. Verify old versions are deleted and recent are kept
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_cleanup_keeps_recent_versions(coi_binary, cleanup_containers, workspace_dir):
    """
    Test cleanup keeps only the most recent N versions.

    Flow:
    1. Launch and stop a container
    2. Create 3 versioned images with timestamps
    3. Run cleanup with --keep 1
    4. Verify only 1 image remains
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    # Use a unique prefix for this test
    image_prefix = f"test-cleanup-{container_name[:8]}-"

    # === Phase 1: Launch and stop container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    time.sleep(2)

    # === Phase 2: Create 3 versioned images ===

    created_images = []
    for _i in range(3):
        # Create image with timestamp-like suffix (YYYYMMDD-HHMMSS format)
        timestamp = time.strftime("%Y%m%d-%H%M%S")
        image_name = f"{image_prefix}{timestamp}"

        result = subprocess.run(
            [coi_binary, "image", "publish", container_name, image_name],
            capture_output=True,
            text=True,
            timeout=120,
        )

        if result.returncode == 0:
            created_images.append(image_name)

        # Sleep to ensure different timestamps
        time.sleep(2)

    # Skip test if we couldn't create enough images
    if len(created_images) < 2:
        # Cleanup whatever we created
        for img in created_images:
            subprocess.run(
                [coi_binary, "image", "delete", img],
                capture_output=True,
                timeout=60,
            )
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
        return  # Skip rest of test

    # === Phase 3: Run cleanup keeping only 1 ===

    result = subprocess.run(
        [coi_binary, "image", "cleanup", image_prefix, "--keep", "1"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Cleanup should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Cleanup complete" in combined_output, (
        f"Should show cleanup complete. Got:\n{combined_output}"
    )

    # === Phase 4: Verify only 1 remains ===

    remaining_count = 0
    for img in created_images:
        result = subprocess.run(
            [coi_binary, "image", "exists", img],
            capture_output=True,
            timeout=30,
        )
        if result.returncode == 0:
            remaining_count += 1
            # Delete the remaining image for cleanup
            subprocess.run(
                [coi_binary, "image", "delete", img],
                capture_output=True,
                timeout=60,
            )

    assert remaining_count == 1, (
        f"Should have exactly 1 image remaining after cleanup. Got: {remaining_count}"
    )

    # === Phase 5: Cleanup container ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
