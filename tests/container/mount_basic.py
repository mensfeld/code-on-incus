"""
Test for coi container mount - mounts directory into container.

Tests that:
1. Launch a container
2. Mount a directory
3. Verify mount is accessible
"""

import os
import subprocess
import tempfile
import time

from support.helpers import (
    calculate_container_name,
)


def test_mount_basic(coi_binary, cleanup_containers, workspace_dir):
    """
    Test basic directory mount into container.

    Flow:
    1. Create a temp directory with a file
    2. Launch a container
    3. Mount the directory
    4. Verify file is accessible inside container
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Create temp directory with test file ===

    with tempfile.TemporaryDirectory() as tmpdir:
        test_file = os.path.join(tmpdir, "mount-test.txt")
        with open(test_file, "w") as f:
            f.write("mount-test-content-123")

        # === Phase 2: Launch container ===

        result = subprocess.run(
            [coi_binary, "container", "launch", "coi", container_name],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, \
            f"Container launch should succeed. stderr: {result.stderr}"

        time.sleep(3)

        # === Phase 3: Mount directory ===
        # Syntax: coi container mount <name> <device-name> <source> <path>

        mount_name = "test-mount"
        result = subprocess.run(
            [coi_binary, "container", "mount", container_name, mount_name, tmpdir, "/mnt/test"],
            capture_output=True,
            text=True,
            timeout=60,
        )

        assert result.returncode == 0, \
            f"Mount should succeed. stderr: {result.stderr}"

        time.sleep(2)

        # === Phase 4: Verify file accessible ===

        result = subprocess.run(
            [coi_binary, "container", "exec", container_name, "--", "cat", "/mnt/test/mount-test.txt"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode == 0, \
            f"Reading mounted file should succeed. stderr: {result.stderr}"

        combined_output = result.stdout + result.stderr
        assert "mount-test-content-123" in combined_output, \
            f"Mounted file should contain expected content. Got:\n{combined_output}"

        # === Phase 5: Cleanup ===

        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
