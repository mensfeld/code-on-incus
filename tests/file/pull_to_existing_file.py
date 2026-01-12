"""
Test for coi file pull - pull to existing local file (overwrite).

Tests that:
1. Launch a container
2. Create a file in container
3. Create a local file with different content
4. Pull and overwrite local file
5. Verify new content
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_to_existing_file(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that pulling overwrites existing local file.

    Flow:
    1. Launch a container
    2. Create a test file in container
    3. Create existing local file with different content
    4. Pull file (should overwrite)
    5. Verify new content replaced old
    6. Cleanup
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

    new_content = "new-content-from-container-99999"
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "sh",
            "-c",
            f"echo '{new_content}' > /tmp/overwrite-test.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"File creation should succeed. stderr: {result.stderr}"

    # === Phase 3: Create existing local file ===

    local_file = os.path.join(workspace_dir, "existing-file.txt")
    old_content = "old-content-should-be-replaced"
    with open(local_file, "w") as f:
        f.write(old_content)

    # === Phase 4: Pull file (overwrite) ===

    result = subprocess.run(
        [coi_binary, "file", "pull", f"{container_name}:/tmp/overwrite-test.txt", local_file],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"File pull should succeed. stderr: {result.stderr}"

    # === Phase 5: Verify new content ===

    with open(local_file) as f:
        content = f.read()

    assert new_content in content, f"File should contain new content. Got: {content}"
    assert old_content not in content, f"Old content should be replaced. Got: {content}"

    # === Phase 6: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
