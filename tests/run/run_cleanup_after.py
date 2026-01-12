"""
Test for coi run - container cleanup after command.

Tests that:
1. Run a command
2. Verify container is cleaned up after
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_run_cleanup_after(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that container is cleaned up after run completes.

    Flow:
    1. Run coi run with specific slot
    2. After completion, verify container doesn't exist
    """
    # Use a specific slot so we know the container name
    slot = 7

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot",
            str(slot),
            "echo",
            "cleanup-test",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    # Wait a moment for cleanup
    time.sleep(2)

    # Calculate what the container name would be
    container_name = calculate_container_name(workspace_dir, slot)

    # Verify container doesn't exist (was cleaned up)
    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, (
        f"Container should be cleaned up after run. stdout: {result.stdout}"
    )
