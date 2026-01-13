"""
Test for coi tmux capture - error when container is stopped.

Tests that:
1. Launch a container
2. Stop the container
3. Try to capture via tmux capture
4. Verify error about stopped container
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_tmux_capture_stopped_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test tmux capture fails gracefully when container is stopped.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Try to use coi tmux capture
    4. Verify error message about container not running
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

    # === Phase 2: Stop the container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name, "--force"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    # === Phase 3: Try to capture from stopped container ===

    result = subprocess.run(
        [coi_binary, "tmux", "capture", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail
    assert result.returncode != 0, "Tmux capture should fail for stopped container"

    combined_output = result.stdout + result.stderr

    # Should show error about container not running
    assert "not running" in combined_output.lower(), (
        f"Should indicate container is not running. Got:\n{combined_output}"
    )

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
