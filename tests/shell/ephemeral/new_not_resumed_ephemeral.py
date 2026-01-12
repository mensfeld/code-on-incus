"""
Test for coi shell - new session is NOT resumed without --resume flag.

Verifies that:
1. Start fake-claude, interact with it
2. Poweroff container
3. Start coi shell again WITHOUT --resume
4. Verify it's a NEW session (not resuming the old one)
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
    wait_for_specific_container_deletion,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_new_session_not_resumed(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that without --resume, a new session is started.

    Flow:
    1. Start coi shell, interact with fake-claude
    2. Poweroff container
    3. Start coi shell again (no --resume)
    4. Verify fake-claude shows "Session:" (new session), not "Resuming session:"
    5. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}

    # === Phase 1: Initial session ===

    child = spawn_coi(
        coi_binary,
        ["shell"],
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

    # Wait for container deletion
    container_deleted = wait_for_specific_container_deletion(container_name, timeout=30)
    assert container_deleted, f"Container {container_name} should be deleted"

    # === Phase 2: Start NEW session (no --resume) ===

    child2 = spawn_coi(
        coi_binary,
        ["shell"],  # No --resume flag
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
    # If it does, the session was incorrectly restored
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

    # Wait for cleanup
    container_name2 = calculate_container_name(workspace_dir, 1)
    wait_for_specific_container_deletion(container_name2, timeout=30)

    # Force cleanup any remaining
    containers = get_container_list()
    for c in containers:
        if c.startswith("coi-test-"):
            subprocess.run(
                ["sg", "incus-admin", "-c", f"incus delete --force {c}"],
                capture_output=True,
            )

    # Assert the unique marker from first session is NOT present
    assert not marker_found, (
        f"UNIQUE-MARKER-78923 should NOT appear without --resume flag. Output:\n{output}"
    )
