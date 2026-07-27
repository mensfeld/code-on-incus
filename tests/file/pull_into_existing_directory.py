"""
Test for coi file pull - existing directory destination means "place inside".
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_into_existing_directory(coi_binary, cleanup_containers, workspace_dir):
    """
    1. Launch a container
    2. Create /tmp/pull-inside-test.txt in the container
    3. Seed the workspace and a subdirectory with sentinel files
    4. Pull to "." -> file lands at ./pull-inside-test.txt, sentinels survive
    5. Pull to "sub" -> file lands at sub/pull-inside-test.txt, sentinels survive
    6. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # 1
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # 2
    test_content = "pull-inside-test-13579"
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "sh",
            "-c",
            f"echo '{test_content}' > /tmp/pull-inside-test.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"File creation should succeed. stderr: {result.stderr}"

    # 3
    sentinel = os.path.join(workspace_dir, "sentinel.txt")
    with open(sentinel, "w") as f:
        f.write("must survive")

    subdir = os.path.join(workspace_dir, "sub")
    os.mkdir(subdir)
    sub_sentinel = os.path.join(subdir, "sub-sentinel.txt")
    with open(sub_sentinel, "w") as f:
        f.write("must survive too")

    # 4
    result = subprocess.run(
        [coi_binary, "file", "pull", f"{container_name}:/tmp/pull-inside-test.txt", "."],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"Pull to '.' should succeed. stderr: {result.stderr}"

    pulled = os.path.join(workspace_dir, "pull-inside-test.txt")
    assert os.path.isfile(pulled), "File should land inside the destination directory"
    with open(pulled) as f:
        assert test_content in f.read(), "Pulled file should have container content"
    assert os.path.isfile(sentinel), "Pre-existing files must survive a pull into a directory"

    # 5
    result = subprocess.run(
        [coi_binary, "file", "pull", f"{container_name}:/tmp/pull-inside-test.txt", "sub"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"Pull to 'sub' should succeed. stderr: {result.stderr}"

    pulled_sub = os.path.join(subdir, "pull-inside-test.txt")
    assert os.path.isfile(pulled_sub), "File should land inside the subdirectory"
    assert os.path.isfile(sub_sentinel), "Pre-existing files in the subdirectory must survive"

    # 6
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
