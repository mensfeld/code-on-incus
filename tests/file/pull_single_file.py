"""
Test for coi file pull - pull a single file from container.

Tests that:
1. Launch a container
2. Create a file in the container
3. Pull file to local filesystem
4. Verify file exists locally with correct content
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_single_file(coi_binary, cleanup_containers, workspace_dir):
    """
    Test pulling a single file from a container.

    Flow:
    1. Launch a container
    2. Create a test file in container
    3. Pull file to local filesystem
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

    # === Phase 2: Create file in container ===

    test_content = "hello-from-pull-test-67890"
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "sh",
            "-c",
            f"echo '{test_content}' > /tmp/test-pull.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"File creation should succeed. stderr: {result.stderr}"

    # === Phase 3: Pull file from container ===

    local_file = os.path.join(workspace_dir, "pulled-file.txt")
    result = subprocess.run(
        [coi_binary, "file", "pull", f"{container_name}:/tmp/test-pull.txt", local_file],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"File pull should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Pulled" in combined_output, f"Should show pull confirmation. Got:\n{combined_output}"

    # === Phase 4: Verify file exists locally ===

    assert os.path.exists(local_file), f"Pulled file should exist at {local_file}"

    with open(local_file) as f:
        content = f.read()

    assert test_content in content, (
        f"File content should match. Expected '{test_content}', got: {content}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
