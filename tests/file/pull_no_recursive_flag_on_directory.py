"""
Test for coi file pull - pulling a remote directory without -r fails clearly.
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_no_recursive_flag_on_directory(coi_binary, cleanup_containers, workspace_dir):
    """
    1. Launch a container
    2. Create /tmp/somedir in the container
    3. Pull it without -r -> non-zero exit, error mentions -r
    4. Cleanup
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
            "mkdir -p /tmp/somedir && echo x > /tmp/somedir/f.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Directory creation should succeed. stderr: {result.stderr}"

    # 3
    result = subprocess.run(
        [coi_binary, "file", "pull", f"{container_name}:/tmp/somedir", "pulled-dir"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Pulling a directory without -r should fail"
    stderr = result.stderr.lower()
    assert "use -r" in stderr or "recursive" in stderr, (
        f"Error should point at -r. stderr: {result.stderr}"
    )
    assert not os.path.exists(os.path.join(workspace_dir, "pulled-dir")), (
        "Nothing should be created for a failed pull"
    )

    # 4
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
