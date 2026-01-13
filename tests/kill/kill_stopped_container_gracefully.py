"""
Test for coi kill - gracefully handle already-stopped containers.

Tests that:
1. Launch a container
2. Stop the container
3. Kill the stopped container
4. Verify kill succeeds (no error for already-stopped container)
"""

import subprocess

from support.helpers import calculate_container_name


def test_kill_stopped_container_gracefully(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that kill command handles already-stopped containers gracefully.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Kill the stopped container
    4. Verify kill succeeds without error

    This tests the fix for: kill should check if container is running
    before trying to stop it, preventing "already stopped" errors.
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

    # === Phase 2: Stop the container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name, "--force"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    # === Phase 3: Kill the already-stopped container ===

    result = subprocess.run(
        [coi_binary, "kill", container_name, "--force"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed even though container was already stopped
    assert result.returncode == 0, (
        f"Kill should succeed for already-stopped container. "
        f"returncode: {result.returncode}, stderr: {result.stderr}"
    )

    combined_output = result.stdout + result.stderr

    # Should show that container was killed
    assert "Killed" in combined_output or "killed" in combined_output.lower(), (
        f"Should show container was killed. Got:\n{combined_output}"
    )

    # === Phase 4: Verify container is gone ===

    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # exists should return non-zero (container should be deleted)
    assert result.returncode != 0, "Container should be deleted after kill"
