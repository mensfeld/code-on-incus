"""
Test for coi tmux send - send commands to a tmux session.

Tests that:
1. Launch a container
2. Create a tmux session inside it
3. Send a command via tmux send
4. Verify command was executed
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_tmux_send_command(coi_binary, cleanup_containers, workspace_dir):
    """
    Test sending commands to a tmux session.

    Flow:
    1. Launch a container
    2. Create a tmux session inside container
    3. Use coi tmux send to send a command
    4. Capture tmux output to verify command executed
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

    # === Phase 2: Create tmux session inside container ===

    tmux_session = f"coi-{container_name}"

    # Start a tmux session with a shell
    # Create the session as the code user (uid 1000 in coi-default), matching
    # what `coi shell --background` produces — tmux sockets are per-user, and
    # the helpers must find the code user's socket, not root's (#588).
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--user",
            "1000",
            "--group",
            "1000",
            "--",
            "tmux",
            "new-session",
            "-d",
            "-s",
            tmux_session,
            "bash",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Tmux session creation should succeed. stderr: {result.stderr}"

    time.sleep(1)

    # === Phase 3: Send command via coi tmux send ===

    result = subprocess.run(
        [coi_binary, "tmux", "send", container_name, "echo TMUX_TEST_MARKER"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Tmux send should succeed. stderr: {result.stderr}"
    assert "Sent command to session" in result.stdout, (
        f"Should confirm command sent. Got:\n{result.stdout}"
    )

    time.sleep(1)

    # === Phase 4: Verify command was executed by capturing tmux output ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            "--user",
            "1000",
            "--group",
            "1000",
            container_name,
            "--",
            "tmux",
            "capture-pane",
            "-t",
            tmux_session,
            "-p",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Tmux capture should succeed. stderr: {result.stderr}"
    assert "TMUX_TEST_MARKER" in result.stdout, (
        f"Command output should appear in tmux pane. Got:\n{result.stdout}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
