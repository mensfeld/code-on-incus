"""
Test for coi file push -r - push directory recursively.

Tests that:
1. Launch a container
2. Create a local directory with files
3. Push directory with -r flag
4. Verify directory and files exist in container
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_push_directory_recursive(coi_binary, cleanup_containers, workspace_dir):
    """
    Test pushing a directory recursively to a container.

    Flow:
    1. Launch a container
    2. Create a test directory with nested files
    3. Push directory with -r flag
    4. Verify files exist in container
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

    # === Phase 2: Create local test directory with files ===

    test_dir = os.path.join(workspace_dir, "push-dir-test")
    os.makedirs(test_dir, exist_ok=True)

    # Create files with unique content
    with open(os.path.join(test_dir, "file1.txt"), "w") as f:
        f.write("content-file1-xyz")

    subdir = os.path.join(test_dir, "subdir")
    os.makedirs(subdir, exist_ok=True)
    with open(os.path.join(subdir, "file2.txt"), "w") as f:
        f.write("content-file2-abc")

    # === Phase 3: Push directory with -r flag ===

    result = subprocess.run(
        [coi_binary, "file", "push", "-r", test_dir, f"{container_name}:/tmp/push-dir-test"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Directory push should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Pushed directory" in combined_output, (
        f"Should show push confirmation. Got:\n{combined_output}"
    )

    # === Phase 4: Verify files exist in container ===

    # Check first file
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "cat",
            "/tmp/push-dir-test/file1.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"file1.txt should exist. stderr: {result.stderr}"
    combined_output = result.stdout + result.stderr
    assert "content-file1-xyz" in combined_output, (
        f"file1.txt content should match. Got:\n{combined_output}"
    )

    # Check nested file
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "cat",
            "/tmp/push-dir-test/subdir/file2.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"subdir/file2.txt should exist. stderr: {result.stderr}"
    combined_output = result.stdout + result.stderr
    assert "content-file2-abc" in combined_output, (
        f"file2.txt content should match. Got:\n{combined_output}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
