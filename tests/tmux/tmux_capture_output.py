"""
Test for coi tmux capture - capture output from a tmux session.

Tests that:
1. Launch a container
2. Create a tmux session with output
3. Capture output via tmux capture
4. Verify output is returned
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_tmux_capture_output(coi_binary, cleanup_containers, workspace_dir):
    """
    Test capturing output from a tmux session.

    Flow:
    1. Launch a container
    2. Create a tmux session
    3. Execute command in tmux to generate output
    4. Use coi tmux capture to capture output
    5. Verify output is correct
    6. Cleanup
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

    # === Phase 2: Create tmux session ===

    tmux_session = f"coi-{container_name}"

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
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

    # === Phase 3: Send command to generate output ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "tmux",
            "send-keys",
            "-t",
            tmux_session,
            "echo CAPTURE_TEST_OUTPUT_12345",
            "Enter",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Sending command should succeed. stderr: {result.stderr}"

    time.sleep(1)

    # === Phase 4: Capture output via coi tmux capture ===

    result = subprocess.run(
        [coi_binary, "tmux", "capture", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Tmux capture should succeed. stderr: {result.stderr}"
    assert "CAPTURE_TEST_OUTPUT_12345" in result.stdout, (
        f"Captured output should contain test marker. Got:\n{result.stdout}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
