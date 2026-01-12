"""
Test for coi container exec --cwd - executes in specified directory.

Tests that:
1. Launch a container
2. Execute command with --cwd flag
3. Verify command runs in that directory
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_exec_with_cwd(coi_binary, cleanup_containers, workspace_dir):
    """
    Test executing command in a specific directory.

    Flow:
    1. Launch a container
    2. Execute pwd with --cwd /tmp
    3. Verify output shows /tmp
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

    # === Phase 2: Execute with --cwd ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--cwd", "/tmp", "--", "pwd"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Exec with --cwd should succeed. stderr: {result.stderr}"

    # === Phase 3: Verify directory ===

    combined_output = result.stdout + result.stderr
    assert "/tmp" in combined_output.strip(), f"Should run in /tmp. Got:\n{combined_output}"

    # === Phase 4: Test another directory ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--cwd", "/home", "--", "pwd"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Exec with --cwd /home should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "/home" in combined_output.strip(), f"Should run in /home. Got:\n{combined_output}"

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
