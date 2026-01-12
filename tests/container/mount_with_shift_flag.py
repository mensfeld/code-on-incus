"""
Test for coi container mount --shift - mounts with UID shifting.

Tests that:
1. Launch a container
2. Mount a directory with --shift flag
3. Verify file ownership appears correct inside container
"""

import os
import subprocess
import tempfile
import time

from support.helpers import (
    calculate_container_name,
)


def test_mount_with_shift_flag(coi_binary, cleanup_containers, workspace_dir):
    """
    Test mount with --shift flag for UID/GID shifting.

    Flow:
    1. Create a temp directory with a file
    2. Launch a container
    3. Mount with --shift flag
    4. Verify file is accessible and ownership is shifted
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Create temp directory with test file ===

    with tempfile.TemporaryDirectory() as tmpdir:
        test_file = os.path.join(tmpdir, "shift-test.txt")
        with open(test_file, "w") as f:
            f.write("shift-test-content")

        # === Phase 2: Launch container ===

        result = subprocess.run(
            [coi_binary, "container", "launch", "coi", container_name],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

        time.sleep(3)

        # === Phase 3: Mount with --shift ===
        # Syntax: coi container mount <name> <device-name> <source> <path>

        mount_name = "shift-mount"
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "mount",
                container_name,
                mount_name,
                tmpdir,
                "/mnt/shifted",
                "--shift",
            ],
            capture_output=True,
            text=True,
            timeout=60,
        )

        assert result.returncode == 0, f"Mount with --shift should succeed. stderr: {result.stderr}"

        time.sleep(2)

        # === Phase 4: Verify file accessible ===

        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "cat",
                "/mnt/shifted/shift-test.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode == 0, (
            f"Reading shifted mount file should succeed. stderr: {result.stderr}"
        )

        combined_output = result.stdout + result.stderr
        assert "shift-test-content" in combined_output, (
            f"Shifted mount file should contain expected content. Got:\n{combined_output}"
        )

        # Check ownership appears as valid user inside container
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "ls",
                "-la",
                "/mnt/shifted/shift-test.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode == 0, f"ls -la should succeed. stderr: {result.stderr}"

        # === Phase 5: Cleanup ===

        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
