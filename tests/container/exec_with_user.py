"""
Test for coi container exec --user - executes as specified user.

Tests that:
1. Launch a container
2. Execute command with --user flag (numeric UID)
3. Verify command runs as that user
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_exec_with_user(coi_binary, cleanup_containers, workspace_dir):
    """
    Test executing command as a specific user.

    Flow:
    1. Launch a container
    2. Execute whoami with --user 0 (root)
    3. Verify output shows root
    4. Execute whoami with --user 1000 (code)
    5. Cleanup
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

    # === Phase 2: Execute as root (UID 0) ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--user", "0", "--", "whoami"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Exec as root should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "root" in combined_output.strip(), f"Should run as root. Got:\n{combined_output}"

    # === Phase 3: Execute as code (UID 1000) ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--user", "1000", "--", "whoami"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # code user exists in coi image with UID 1000
    assert result.returncode == 0, f"Exec as code should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "code" in combined_output.strip(), f"Should run as code. Got:\n{combined_output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
