"""
Test for coi shell - ephemeral resume does NOT persist home directory files.

Verifies that:
1. Start fake-claude, exit to bash
2. Create a file ~/test.txt in container
3. Poweroff container
4. Resume session
5. The file ~/test.txt should NOT exist (only .claude is restored, not home dir)
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
    wait_for_text_on_screen,
    with_live_screen,
)


def test_resume_does_not_persist_home_files(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that ephemeral resume only restores .claude, not other home files.

    Flow:
    1. Start coi shell
    2. Exit claude to bash
    3. Create ~/test.txt file
    4. Poweroff
    5. Resume session
    6. Verify ~/test.txt does NOT exist
    7. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}

    # === Phase 1: Create file in container ===

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

    # Quick interaction with fake-claude
    with with_live_screen(child) as monitor:
        time.sleep(2)
        send_prompt(child, "init session")
        responded = wait_for_text_in_monitor(monitor, "init session-BACK", timeout=30)
        assert responded, "Fake claude should respond"

    # Exit claude to bash
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # Create a test file in home directory
    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("touch ~/test.txt && echo FILE_CREATED_$((99+1))")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        # 99 + 1 = 100
        created = wait_for_text_in_monitor(monitor, "FILE_CREATED_100", timeout=10)
        assert created, "Should create test file"

    # Verify file exists
    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("test -f ~/test.txt && echo EXISTS_$((200+22))")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        # 200 + 22 = 222
        exists = wait_for_text_in_monitor(monitor, "EXISTS_222", timeout=10)
        assert exists, "File should exist before poweroff"

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

    # === Phase 2: Resume and verify file is gone ===

    child2 = spawn_coi(
        coi_binary,
        ["shell", "--resume"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child2, timeout=60)

    # Wait for resume
    try:
        wait_for_text_on_screen(child2, "Resuming session", timeout=30)
    except TimeoutError:
        pass  # Continue anyway to check file

    time.sleep(2)
    # Exit claude to bash
    child2.send("exit")
    time.sleep(1)
    child2.send("\x0d")
    time.sleep(2)

    # Check that file does NOT exist
    with with_live_screen(child2) as monitor:
        time.sleep(1)
        child2.send("test -f ~/test.txt && echo FILE_EXISTS || echo FILE_GONE_$((333+111))")
        time.sleep(0.3)
        child2.send("\x0d")
        time.sleep(1)
        # 333 + 111 = 444
        file_gone = wait_for_text_in_monitor(monitor, "FILE_GONE_444", timeout=10)

    # Get output for debugging
    if hasattr(child2.logfile_read, "get_raw_output"):
        output = child2.logfile_read.get_raw_output()
    elif hasattr(child2.logfile_read, "get_display_stripped"):
        output = child2.logfile_read.get_display_stripped()
    else:
        output = ""

    # Cleanup
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

    # Assert file was NOT persisted
    assert file_gone, (
        f"~/test.txt should NOT exist after resume (only .claude is restored). Output:\n{output}"
    )
