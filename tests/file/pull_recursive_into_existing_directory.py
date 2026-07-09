"""
Test for coi file pull -r - existing directory destination means "place inside".
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_recursive_into_existing_directory(coi_binary, cleanup_containers, workspace_dir):
    """
    1. Launch a container
    2. Create /tmp/datadir/{a.txt,nested/b.txt} in the container
    3. Seed an existing destination directory with a sentinel
    4. Pull -r to that directory -> tree lands at <dest>/datadir/...
    5. Sentinel survives
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
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "sh",
            "-c",
            "mkdir -p /tmp/datadir/nested"
            " && echo 'content-a' > /tmp/datadir/a.txt"
            " && echo 'content-b' > /tmp/datadir/nested/b.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Tree creation should succeed. stderr: {result.stderr}"

    # 3
    dest = os.path.join(workspace_dir, "downloads")
    os.mkdir(dest)
    sentinel = os.path.join(dest, "sentinel.txt")
    with open(sentinel, "w") as f:
        f.write("must survive")

    # 4
    result = subprocess.run(
        [coi_binary, "file", "pull", "-r", f"{container_name}:/tmp/datadir", "downloads"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"Recursive pull should succeed. stderr: {result.stderr}"

    pulled_a = os.path.join(dest, "datadir", "a.txt")
    pulled_b = os.path.join(dest, "datadir", "nested", "b.txt")
    assert os.path.isfile(pulled_a), "Pulled tree should land inside the destination directory"
    assert os.path.isfile(pulled_b), "Nested files should be pulled"
    with open(pulled_a) as f:
        assert "content-a" in f.read()

    # 5
    with open(sentinel) as f:
        assert f.read() == "must survive", "Pre-existing files must survive a pull into a directory"

    # 6
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
