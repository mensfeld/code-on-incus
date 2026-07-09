"""
Test for coi file pull - "../" destination

Historically, `coi file pull <c>:/tmp/example.output ../` from inside a
project directory recursively deleted the parent directory
(os.RemoveAll on the destination) before failing with a confusing rename error.
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_parent_dir_destination(coi_binary, cleanup_containers, workspace_dir):
    """
    1. Launch a container and create /tmp/example.output in it
    2. Build workspace/project (cwd) and workspace/other-project (sibling
       sentinel tree, standing in for the user's other checkouts)
    3. From workspace/project, pull to "../"
    4. Assert: file lands at workspace/example.output; project and sibling
       trees are fully intact
    5. Cleanup
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

    test_content = "incident-replay-24680"
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "sh",
            "-c",
            f"echo '{test_content}' > /tmp/example.output",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"File creation should succeed. stderr: {result.stderr}"

    # 2
    project = os.path.join(workspace_dir, "project")
    os.mkdir(project)
    project_file = os.path.join(project, "main.go")
    with open(project_file, "w") as f:
        f.write("package main")

    sibling = os.path.join(workspace_dir, "other-project")
    os.makedirs(os.path.join(sibling, "src"))
    sibling_file = os.path.join(sibling, "src", "keep.txt")
    with open(sibling_file, "w") as f:
        f.write("must survive")

    # 3
    result = subprocess.run(
        [coi_binary, "file", "pull", f"{container_name}:/tmp/example.output", "../"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=project,
    )
    assert result.returncode == 0, f"Pull to '../' should succeed. stderr: {result.stderr}"

    # 4
    pulled = os.path.join(workspace_dir, "example.output")
    assert os.path.isfile(pulled), "File should land inside the parent directory"
    with open(pulled) as f:
        assert test_content in f.read(), "Pulled file should have container content"

    assert os.path.isfile(project_file), "The invoking project directory must survive"
    assert os.path.isfile(sibling_file), "Sibling directories must survive"
    with open(sibling_file) as f:
        assert f.read() == "must survive", "Sibling file content must be untouched"

    # 5
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
