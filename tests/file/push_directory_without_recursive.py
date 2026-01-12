"""
Test for coi file push - push directory without -r flag.

Tests that:
1. Launch a container
2. Create a local directory
3. Try to push directory without -r flag
4. Verify error message about needing -r flag
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_push_directory_without_recursive(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that pushing a directory without -r flag fails with helpful message.

    Flow:
    1. Launch a container
    2. Create a test directory locally
    3. Try to push without -r flag
    4. Verify error message mentions -r flag
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

    # === Phase 2: Create local test directory ===

    test_dir = os.path.join(workspace_dir, "test-dir")
    os.makedirs(test_dir, exist_ok=True)
    with open(os.path.join(test_dir, "file.txt"), "w") as f:
        f.write("content")

    # === Phase 3: Try to push directory without -r ===

    result = subprocess.run(
        [coi_binary, "file", "push", test_dir, f"{container_name}:/tmp/test-dir"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 4: Verify failure with helpful message ===

    assert result.returncode != 0, f"Push directory without -r should fail. stdout: {result.stdout}"

    combined_output = result.stdout + result.stderr
    assert "-r" in combined_output or "recursive" in combined_output.lower(), (
        f"Should mention -r flag. Got:\n{combined_output}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
