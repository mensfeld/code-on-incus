"""
Test for coi file pull -r - pull directory recursively.

Tests that:
1. Launch a container
2. Create a directory with files in container
3. Pull directory with -r flag
4. Verify directory and files exist locally
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_directory_recursive(coi_binary, cleanup_containers, workspace_dir):
    """
    Test pulling a directory recursively from a container.

    Flow:
    1. Launch a container
    2. Create a test directory with nested files in container
    3. Pull directory with -r flag
    4. Verify files exist locally
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

    # === Phase 2: Create directory with files in container ===

    # Create directory structure
    commands = [
        "mkdir -p /tmp/pull-dir-test/subdir",
        "echo 'content-file1-pull' > /tmp/pull-dir-test/file1.txt",
        "echo 'content-file2-pull' > /tmp/pull-dir-test/subdir/file2.txt",
    ]

    for cmd in commands:
        result = subprocess.run(
            [coi_binary, "container", "exec", container_name, "--", "sh", "-c", cmd],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert result.returncode == 0, f"Command '{cmd}' should succeed. stderr: {result.stderr}"

    # === Phase 3: Pull directory with -r flag ===

    local_dir = os.path.join(workspace_dir, "pulled-dir")
    result = subprocess.run(
        [coi_binary, "file", "pull", "-r", f"{container_name}:/tmp/pull-dir-test", local_dir],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Directory pull should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Pulled directory" in combined_output, (
        f"Should show pull confirmation. Got:\n{combined_output}"
    )

    # === Phase 4: Verify files exist locally ===

    # Check first file
    file1_path = os.path.join(local_dir, "file1.txt")
    assert os.path.exists(file1_path), f"file1.txt should exist at {file1_path}"
    with open(file1_path) as f:
        content = f.read()
    assert "content-file1-pull" in content, f"file1.txt content should match. Got: {content}"

    # Check nested file
    file2_path = os.path.join(local_dir, "subdir", "file2.txt")
    assert os.path.exists(file2_path), f"subdir/file2.txt should exist at {file2_path}"
    with open(file2_path) as f:
        content = f.read()
    assert "content-file2-pull" in content, f"file2.txt content should match. Got: {content}"

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
