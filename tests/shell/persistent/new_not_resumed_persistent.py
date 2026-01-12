"""
Test for coi shell --persistent - new session is NOT resumed without --resume flag.

Verifies that:
1. Start fake-claude in persistent mode, interact with it
2. Poweroff container (container kept in persistent mode)
3. Delete container for clean slate
4. Start coi shell --persistent again WITHOUT --resume
5. Verify it's a NEW session (not resuming the old one)
"""

import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    get_container_list,
    send_prompt,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_persistent_new_session_not_resumed(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that without --resume, a new persistent session is started.

    Flow:
    1. Start coi shell --persistent, interact with fake-claude
    2. Poweroff container (kept in persistent mode)
    3. Delete container to ensure clean slate
    4. Start coi shell --persistent again (no --resume)
    5. Verify fake-claude shows new session, not resuming
    6. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}

    # === Phase 1: Initial persistent session ===

    child = spawn_coi(
        coi_binary,
        ["shell", "--persistent"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    container_name = calculate_container_name(workspace_dir, 1)

    # Interact with fake-claude - send a unique marker
    with with_live_screen(child) as monitor:
        time.sleep(2)
        send_prompt(child, "UNIQUE-MARKER-78923")
        responded = wait_for_text_in_monitor(monitor, "UNIQUE-MARKER-78923-BACK", timeout=30)
        assert responded, "Fake claude should respond"

    # Exit claude to bash
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # Poweroff container
    child.send("sudo poweroff")
    time.sleep(0.3)
    child.send("\x0d")

    try:
        child.expect(EOF, timeout=60)
    except TIMEOUT:
        pass

    try:
        child.close(force=False)
    except Exception:
        child.close(force=True)

    # Give time for cleanup
    time.sleep(3)

    # In persistent mode, container is kept - delete it for clean slate
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
    time.sleep(1)

    # === Phase 2: Start NEW persistent session (no --resume) ===

    child2 = spawn_coi(
        coi_binary,
        ["shell", "--persistent"],  # No --resume flag
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child2, timeout=60)
    wait_for_prompt(child2, timeout=90)

    # Get raw output to check if old session was restored
    if hasattr(child2.logfile_read, "get_raw_output"):
        output = child2.logfile_read.get_raw_output()
    else:
        output = ""

    # The unique marker from first session should NOT appear
    marker_found = "UNIQUE-MARKER-78923" in output

    # Cleanup
    child2.send("exit")
    time.sleep(0.3)
    child2.send("\x0d")
    time.sleep(2)

    child2.send("sudo poweroff")
    time.sleep(0.3)
    child2.send("\x0d")

    try:
        child2.expect(EOF, timeout=60)
    except TIMEOUT:
        pass

    try:
        child2.close(force=False)
    except Exception:
        child2.close(force=True)

    # Give time for cleanup
    time.sleep(3)

    # Force cleanup
    container_name2 = calculate_container_name(workspace_dir, 1)
    subprocess.run(
        [coi_binary, "container", "delete", container_name2, "--force"],
        capture_output=True,
        timeout=30,
    )

    # Verify container is gone
    time.sleep(1)
    containers = get_container_list()
    assert container_name2 not in containers, (
        f"Container {container_name2} should be deleted after cleanup"
    )

    # Assert the unique marker from first session is NOT present
    assert not marker_found, (
        f"UNIQUE-MARKER-78923 should NOT appear without --resume flag. Output:\n{output}"
    )
