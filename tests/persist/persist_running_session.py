"""
Test converting a running ephemeral session to persistent.

Tests the main use case:
1. Start ephemeral shell session (no --persistent flag)
2. While session is running, persist it from outside
3. Verify coi list shows (persistent)
4. Exit the shell
5. Verify container still exists (wasn't deleted despite starting ephemeral)
6. Restart and verify content is preserved
"""

import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_persist_running_session(coi_binary, cleanup_containers, workspace_dir):
    """
    Test persisting a running ephemeral session.

    This verifies the main user story:
    - Start ephemeral shell
    - Decide you want to keep it
    - Run coi persist while it's still running
    - Exit normally
    - Container should still exist
    """
    container_name = calculate_container_name(workspace_dir, 1)
    marker_file = "/home/code/test_marker.txt"
    marker_content = "Content that should be preserved"

    # Use dummy CLI to avoid onboarding issues
    env = {"COI_USE_DUMMY": "1"}

    # === Phase 1: Start ephemeral shell and create content ===

    child = spawn_coi(coi_binary, ["shell", "--workspace", workspace_dir, "--slot", "1"], env=env)

    wait_for_container_ready(child, timeout=90)
    wait_for_prompt(child, timeout=90)

    # Exit to bash
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # Create a test file
    with with_live_screen(child) as monitor:
        child.send(f"echo '{marker_content}' > {marker_file}")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)

        # Verify file was created
        child.send(f"cat {marker_file}")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)

        file_created = wait_for_text_in_monitor(monitor, marker_content, timeout=10)
        assert file_created, "File should be created successfully"

    # === Phase 2: While session is running, persist from outside ===

    result = subprocess.run(
        [coi_binary, "persist", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Persist should succeed. stderr: {result.stderr}"

    # === Phase 3: Verify coi list shows (persistent) ===

    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"

    list_output = result.stdout + result.stderr
    assert "(persistent)" in list_output, (
        f"Container should show as persistent in list. Got:\n{list_output}"
    )

    # === Phase 4: Exit the shell normally ===

    # Just kill the process (simulating Ctrl+C)
    child.sendcontrol("c")
    time.sleep(0.5)
    child.sendcontrol("c")
    time.sleep(0.5)

    try:
        child.expect(EOF, timeout=30)
    except TIMEOUT:
        pass

    try:
        child.close(force=False)
    except Exception:
        child.close(force=True)

    time.sleep(5)

    # === Phase 5: Verify container still exists ===

    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, (
        f"Container should still exist after exiting ephemeral session that was persisted. "
        f"stderr: {result.stderr}"
    )

    # === Phase 5b: Verify coi list STILL shows (persistent) after exit ===

    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List should succeed after exit. stderr: {result.stderr}"

    list_output_after = result.stdout + result.stderr
    assert container_name in list_output_after, (
        f"Container should still be in list after exit. Got:\n{list_output_after}"
    )
    assert "(persistent)" in list_output_after, (
        f"Container should STILL show as (persistent) after exit. Got:\n{list_output_after}"
    )

    # === Phase 6: Verify data is actually preserved ===

    # Start the container if stopped
    subprocess.run(
        [coi_binary, "container", "start", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    time.sleep(2)

    # Use exec to check file directly in the persisted container
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "cat", marker_file],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"File should exist in persisted container. stderr: {result.stderr}"
    )

    # Check both stdout and stderr for the content (output location varies)
    combined_output = result.stdout + result.stderr
    assert marker_content in combined_output, (
        f"File content should be preserved. Expected '{marker_content}', "
        f"got stdout: {result.stdout}, stderr: {result.stderr}"
    )
