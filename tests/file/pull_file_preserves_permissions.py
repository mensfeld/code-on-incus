"""
Test for coi file pull - pulled file has reasonable permissions.

Tests that:
1. Launch a container
2. Create a file in container
3. Pull file to local filesystem
4. Verify file is readable
"""

import os
import stat
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_file_permissions(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that pulled files have reasonable permissions.

    Flow:
    1. Launch a container
    2. Create a test file in container
    3. Pull file to local filesystem
    4. Verify file is readable
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

    # === Phase 2: Create file in container ===

    test_content = "permission-test-content"
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "sh",
            "-c",
            f"echo '{test_content}' > /tmp/perm-test.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"File creation should succeed. stderr: {result.stderr}"

    # === Phase 3: Pull file from container ===

    local_file = os.path.join(workspace_dir, "perm-test.txt")
    result = subprocess.run(
        [coi_binary, "file", "pull", f"{container_name}:/tmp/perm-test.txt", local_file],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"File pull should succeed. stderr: {result.stderr}"

    # === Phase 4: Verify file is readable ===

    assert os.path.exists(local_file), f"Pulled file should exist at {local_file}"

    file_stat = os.stat(local_file)
    mode = file_stat.st_mode

    # File should be readable by owner
    assert mode & stat.S_IRUSR, f"File should be readable by owner. Mode: {oct(mode)}"

    # Should be able to read content
    with open(local_file) as f:
        content = f.read()

    assert test_content in content, f"Should be able to read file content. Got: {content}"

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
