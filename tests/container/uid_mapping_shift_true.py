"""
Test for UID mapping bug with shift=true workspace bind mounts.

Demonstrates that shift=true fails when the host user's UID does not match the
container's code user UID (1000). On CI runners where the host UID is typically
1001, the code user cannot read or write files in the workspace.

Tests that:
1. Launch a container
2. Mount a temp directory with shift=true as workspace
3. As code user (UID 1000), attempt to read a host-created file
4. As code user, attempt to create a new file
5. As code user, attempt to overwrite the host-created file

Expected: All write operations succeed (shift=true should handle UID mapping).
Actual (when host UID != 1000): Write operations fail — this is the bug.
"""

import os
import subprocess
import tempfile
import time

import pytest

from support.helpers import (
    calculate_container_name,
)


@pytest.mark.xfail(
    os.getuid() != 1000,
    reason="shift=true fails when host UID != container code user UID (1000). "
    "This is a known Incus limitation — raw.idmap is the correct workaround.",
    strict=True,
)
def test_workspace_write_access_shift_true(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that shift=true workspace mounts allow the code user to read/write files.

    This test reproduces a known bug: shift=true only works when the host UID
    matches the container's code user UID (1000). On hosts with a different UID
    (e.g., CI runners at UID 1001), all write operations fail.

    Flow:
    1. Create a temp directory with a host-owned file
    2. Launch a container
    3. Mount the directory with --shift at /workspace
    4. Exec as code user (UID 1000) to read, create, and overwrite files
    5. Assert all operations succeed
    6. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    host_uid = os.getuid()

    # === Phase 1: Create temp directory with a host-owned file ===

    with tempfile.TemporaryDirectory() as tmpdir:
        test_file = os.path.join(tmpdir, "host-file.txt")
        with open(test_file, "w") as f:
            f.write("written-by-host")

        # === Phase 2: Launch container ===

        result = subprocess.run(
            [coi_binary, "container", "launch", "coi-default", container_name],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

        time.sleep(3)

        # === Phase 3: Mount with --shift ===

        result = subprocess.run(
            [
                coi_binary,
                "container",
                "mount",
                container_name,
                "workspace",
                tmpdir,
                "/workspace",
                "--shift",
            ],
            capture_output=True,
            text=True,
            timeout=60,
        )

        assert result.returncode == 0, f"Mount with --shift should succeed. stderr: {result.stderr}"

        time.sleep(2)

        # === Phase 4: Test read/write as code user (UID 1000) ===

        # 4a. Read the host-created file
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--user",
                "1000",
                "--group",
                "1000",
                "--",
                "cat",
                "/workspace/host-file.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode == 0, (
            f"Code user (UID 1000) should be able to read host file with shift=true "
            f"(host UID={host_uid}). stderr: {result.stderr}"
        )
        assert "written-by-host" in result.stdout + result.stderr, (
            f"Host file should contain expected content. Got: {result.stdout + result.stderr}"
        )

        # 4b. Create a new file
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--user",
                "1000",
                "--group",
                "1000",
                "--",
                "sh",
                "-c",
                "echo created-by-code > /workspace/code-file.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode == 0, (
            f"Code user (UID 1000) should be able to create files with shift=true "
            f"(host UID={host_uid}). stderr: {result.stderr}"
        )

        # 4c. Overwrite the host-created file
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--user",
                "1000",
                "--group",
                "1000",
                "--",
                "sh",
                "-c",
                "echo overwritten-by-code > /workspace/host-file.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode == 0, (
            f"Code user (UID 1000) should be able to overwrite host files with shift=true "
            f"(host UID={host_uid}). stderr: {result.stderr}"
        )

        # === Phase 5: Cleanup ===

        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
