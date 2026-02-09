"""
Test for coi container exec -t - PTY allocation.

Tests that:
1. Launch a container
2. Execute command with -t flag
3. Verify PTY is allocated
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_exec_with_tty(coi_binary, cleanup_containers, workspace_dir):
    """
    Test PTY allocation with -t flag.

    Flow:
    1. Launch a container
    2. Execute command with -t to allocate PTY
    3. Verify tty test command succeeds (exits 0 when stdin is a tty)
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

    # === Phase 2: Execute with PTY allocation ===
    # The 'test -t 0' command checks if stdin (fd 0) is a terminal
    # It returns 0 (success) if stdin is a tty, 1 otherwise

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "-t", "--", "test", "-t", "0"],
        capture_output=True,
        text=True,
        timeout=30,
        # Provide stdin to avoid stdin being closed
        input="",
    )

    # With -t flag, stdin should be a PTY, so 'test -t 0' should succeed (exit 0)
    assert result.returncode == 0, (
        f"With -t flag, stdin should be a tty. "
        f"Exit code: {result.returncode}, stderr: {result.stderr}"
    )

    # === Phase 3: Verify without -t flag, stdin is NOT a tty ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "test", "-t", "0"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Without -t flag, stdin should NOT be a PTY, so 'test -t 0' should fail (exit 1)
    assert result.returncode != 0, (
        f"Without -t flag, stdin should NOT be a tty. Exit code: {result.returncode}"
    )

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
