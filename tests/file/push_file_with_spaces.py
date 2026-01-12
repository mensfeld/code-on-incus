"""
Test for coi file push - push file with spaces in name.

Tests that:
1. Launch a container
2. Create a local file with spaces in filename
3. Push file to container
4. Verify file exists with correct content
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_push_file_with_spaces(coi_binary, cleanup_containers, workspace_dir):
    """
    Test pushing a file with spaces in its name.

    Flow:
    1. Launch a container
    2. Create a test file with spaces in name
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

    # === Phase 2: Create local file with spaces ===

    test_content = "content-with-spaces-file-77777"
    local_file = os.path.join(workspace_dir, "file with spaces.txt")
    with open(local_file, "w") as f:
        f.write(test_content)

    # === Phase 3: Push file to container ===

    result = subprocess.run(
        [coi_binary, "file", "push", local_file, f"{container_name}:/tmp/file with spaces.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"File push should succeed. stderr: {result.stderr}"

    # === Phase 4: Verify file exists in container ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "cat", "/tmp/file with spaces.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"File should exist in container. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert test_content in combined_output, f"File content should match. Got:\n{combined_output}"

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
