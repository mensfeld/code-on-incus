"""
Test for coi container exec - close command as poweroff alias.

Tests that:
1. Launch a container
2. Execute 'close' as the code user (should behave like poweroff)
3. Verify command succeeds (exit code 0)
4. Verify container stops cleanly
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_exec_close_alias_poweroff(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that the 'close' command works as an alias for poweroff.

    The 'close' command is a safe alternative to 'poweroff' that doesn't
    exist on the host machine, preventing accidental host shutdowns.

    Flow:
    1. Launch a container
    2. Execute 'close' as code user (should trigger poweroff)
    3. Verify command succeeds
    4. Verify container stops
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 2: Execute 'close' (poweroff alias, no password required) ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--user",
            "1000",
            "--",
            "close",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # close should succeed (exit code 0) just like poweroff
    assert result.returncode == 0, (
        f"close should succeed as poweroff alias. stderr: {result.stderr}"
    )

    # === Phase 3: Wait for container to stop ===

    time.sleep(5)

    # === Phase 4: Verify container stopped ===

    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # running command should return non-zero if container is not running
    assert result.returncode != 0, (
        "Container should be stopped after close. "
        f"Exit code: {result.returncode}, Output: {result.stdout + result.stderr}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
