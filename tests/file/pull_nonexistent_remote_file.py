"""
Test for coi file pull - pull nonexistent remote file.

Tests that:
1. Launch a container
2. Try to pull a file that doesn't exist in container
3. Verify appropriate error message
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_nonexistent_remote_file(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that pulling a nonexistent remote file fails gracefully.

    Flow:
    1. Launch a container
    2. Try to pull nonexistent file
    3. Verify error message
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

    # === Phase 2: Try to pull nonexistent file ===

    local_file = os.path.join(workspace_dir, "should-not-exist.txt")
    result = subprocess.run(
        [coi_binary, "file", "pull", f"{container_name}:/nonexistent/path/file.txt", local_file],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 3: Verify failure ===

    assert result.returncode != 0, f"Pull of nonexistent file should fail. stdout: {result.stdout}"

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "failed" in combined_output
        or "not found" in combined_output
        or "no such file" in combined_output
    ), f"Should show error message. Got:\n{result.stdout + result.stderr}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
