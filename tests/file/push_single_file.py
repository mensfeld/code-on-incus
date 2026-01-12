"""
Test for coi file push - push a single file to container.

Tests that:
1. Launch a container
2. Create a local file
3. Push file to container
4. Verify file exists in container with correct content
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_push_single_file(coi_binary, cleanup_containers, workspace_dir):
    """
    Test pushing a single file to a container.

    Flow:
    1. Launch a container
    2. Create a test file locally
    3. Push file to container
    4. Verify file exists with correct content
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

    # === Phase 2: Create local test file ===

    test_content = "hello-from-push-test-12345"
    local_file = os.path.join(workspace_dir, "test-push.txt")
    with open(local_file, "w") as f:
        f.write(test_content)

    # === Phase 3: Push file to container ===

    result = subprocess.run(
        [coi_binary, "file", "push", local_file, f"{container_name}:/tmp/test-push.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"File push should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Pushed file" in combined_output, (
        f"Should show push confirmation. Got:\n{combined_output}"
    )

    # === Phase 4: Verify file exists in container ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "cat", "/tmp/test-push.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"File should exist in container. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert test_content in combined_output, (
        f"File content should match. Expected '{test_content}', got:\n{combined_output}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
