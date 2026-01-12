"""
Test for coi container exec - propagates exit code from failed command.

Tests that:
1. Launch a container
2. Execute a command that fails
3. Verify exit code is propagated
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_exec_failed_command(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that exit codes from failed commands are propagated.

    Flow:
    1. Launch a container
    2. Execute a command that will fail (exit 42)
    3. Verify exit code is returned
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

    # === Phase 2: Execute failing command ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "sh", "-c", "exit 42"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 3: Verify non-zero exit code ===

    assert result.returncode != 0, "Failed command should return non-zero exit code"

    # === Phase 4: Test command not found ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "nonexistent-command-12345"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, "Non-existent command should return non-zero exit code"

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
