"""
Test for coi file push - push to existing remote file (overwrite).

Tests that:
1. Launch a container
2. Create a file in container
3. Push a different file to same location
4. Verify content is overwritten
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_push_to_existing_file(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that pushing overwrites existing remote file.

    Flow:
    1. Launch a container
    2. Create a file in container
    3. Push new file to same path
    4. Verify content was replaced
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

    # === Phase 2: Create existing file in container ===

    old_content = "old-content-should-be-replaced"
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "sh",
            "-c",
            f"echo '{old_content}' > /tmp/overwrite-test.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Initial file creation should succeed. stderr: {result.stderr}"

    # === Phase 3: Push new file to same location ===

    new_content = "new-content-from-push-88888"
    local_file = os.path.join(workspace_dir, "new-file.txt")
    with open(local_file, "w") as f:
        f.write(new_content)

    result = subprocess.run(
        [coi_binary, "file", "push", local_file, f"{container_name}:/tmp/overwrite-test.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"File push should succeed. stderr: {result.stderr}"

    # === Phase 4: Verify content was replaced ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "cat", "/tmp/overwrite-test.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined_output = result.stdout + result.stderr
    assert new_content in combined_output, (
        f"File should contain new content. Got:\n{combined_output}"
    )
    assert old_content not in combined_output, (
        f"Old content should be replaced. Got:\n{combined_output}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
