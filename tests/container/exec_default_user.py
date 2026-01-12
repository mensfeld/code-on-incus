"""
Test for coi image - verifies 'code' user exists with correct setup.

Tests that:
1. Launch a container
2. Verify 'code' user exists with UID 1000
3. Verify home directory /home/code exists
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_code_user_exists(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that the coi image has the 'code' user configured correctly.

    Flow:
    1. Launch a container
    2. Verify 'code' user exists with UID 1000
    3. Verify home directory /home/code exists
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

    # === Phase 2: Verify code user exists with UID 1000 ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "id", "code"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"User 'code' should exist. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "uid=1000" in combined_output, (
        f"User 'code' should have UID 1000. Got: {combined_output}"
    )

    # === Phase 3: Check home directory exists ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ls", "-d", "/home/code"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, (
        f"Home directory /home/code should exist. stderr: {result.stderr}"
    )

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
