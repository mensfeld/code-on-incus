"""
Test for coi tmux list - list active tmux sessions.

Tests that:
1. Launch two containers
2. Create tmux sessions in both
3. List tmux sessions
4. Verify both sessions are listed
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_tmux_list_sessions(coi_binary, cleanup_containers, workspace_dir):
    """
    Test listing active tmux sessions.

    Flow:
    1. Launch two containers
    2. Create tmux session in each
    3. Use coi tmux list
    4. Verify both sessions are listed
    5. Cleanup
    """
    container1 = calculate_container_name(workspace_dir, 1)
    container2 = calculate_container_name(workspace_dir, 2)

    # === Phase 1: Launch first container and create tmux session ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container1],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Container 1 launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    tmux_session1 = f"coi-{container1}"

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container1,
            "--",
            "tmux",
            "new-session",
            "-d",
            "-s",
            tmux_session1,
            "bash",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Tmux session 1 creation should succeed. stderr: {result.stderr}"
    )

    # === Phase 2: Launch second container and create tmux session ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container2],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Container 2 launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    tmux_session2 = f"coi-{container2}"

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container2,
            "--",
            "tmux",
            "new-session",
            "-d",
            "-s",
            tmux_session2,
            "bash",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Tmux session 2 creation should succeed. stderr: {result.stderr}"
    )

    time.sleep(1)

    # === Phase 3: List tmux sessions ===

    result = subprocess.run(
        [coi_binary, "tmux", "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Tmux list should succeed. stderr: {result.stderr}"
    assert "Active sessions:" in result.stdout, (
        f"Should show sessions header. Got:\n{result.stdout}"
    )

    # Both containers should be listed
    assert container1 in result.stdout, f"Container 1 should be listed. Got:\n{result.stdout}"
    assert container2 in result.stdout, f"Container 2 should be listed. Got:\n{result.stdout}"

    # Both tmux sessions should be mentioned
    assert tmux_session1 in result.stdout, f"Tmux session 1 should be listed. Got:\n{result.stdout}"
    assert tmux_session2 in result.stdout, f"Tmux session 2 should be listed. Got:\n{result.stdout}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container1, "--force"],
        capture_output=True,
        timeout=30,
    )

    subprocess.run(
        [coi_binary, "container", "delete", container2, "--force"],
        capture_output=True,
        timeout=30,
    )
