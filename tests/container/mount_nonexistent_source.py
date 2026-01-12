"""
Test for coi container mount - fails for nonexistent source directory.

Tests that:
1. Launch a container
2. Try to mount nonexistent directory
3. Verify command fails
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_mount_nonexistent_source(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that mounting nonexistent source directory fails.

    Flow:
    1. Launch a container
    2. Try to mount a nonexistent source directory
    3. Verify command fails with appropriate error
    4. Cleanup
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

    # === Phase 2: Try to mount nonexistent source ===
    # Syntax: coi container mount <name> <device-name> <source> <path>

    nonexistent_source = "/nonexistent/path/12345"
    mount_name = "bad-mount"
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "mount",
            container_name,
            mount_name,
            nonexistent_source,
            "/mnt/test",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    # === Phase 3: Verify failure ===

    assert result.returncode != 0, "Mounting nonexistent source should fail"

    combined_output = result.stdout + result.stderr
    has_error = (
        "not found" in combined_output.lower()
        or "does not exist" in combined_output.lower()
        or "no such" in combined_output.lower()
        or "error" in combined_output.lower()
        or "invalid" in combined_output.lower()
    )

    assert has_error, f"Should indicate source not found. Got:\n{combined_output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
