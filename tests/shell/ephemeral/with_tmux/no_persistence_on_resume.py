"""
Test that ephemeral mode does not persist files, even when using --resume.

Flow:
1. Start first ephemeral session (without --persistent)
2. Create test file in ~/
3. Exit session (container should be deleted)
4. Resume session with --resume
5. Verify test file does NOT exist (new container, no persistence)
6. Exit session

Expected:
- First container is deleted after exit (ephemeral mode)
- Resume creates a NEW container (not reusing old one)
- Files from first session do NOT persist to resumed session
- This ensures ephemeral mode is truly ephemeral
"""

import time

from support.helpers import (
    assert_clean_exit,
    exit_claude,
    send_prompt,
    spawn_coi,
    wait_for_container_deletion,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_ephemeral_no_persistence_on_resume(coi_binary, cleanup_containers, workspace_dir):
    """Test that ephemeral mode does not persist files, even when resuming."""

    # First session WITHOUT --persistent (ephemeral mode)
    child = spawn_coi(coi_binary, ["shell", "--tmux=true"], cwd=workspace_dir)

    wait_for_container_ready(child)
    wait_for_prompt(child)

    with with_live_screen(child) as monitor:
        time.sleep(2)

        # Create test file in home directory
        send_prompt(child, "mkdir -p ~/ephemeral_test && echo 'should-not-persist' > ~/ephemeral_test/data.txt")
        send_prompt(child, "Print ONLY result of sum of 3000 and 4000 and NOTHING ELSE")
        file_created = wait_for_text_in_monitor(monitor, "7000", timeout=30)
        assert file_created, "Failed to create test file in first session"

        # Exit first session (container should be deleted)
        time.sleep(1)
        clean_exit = exit_claude(child)
        wait_for_container_deletion()

    assert_clean_exit(clean_exit, child)

    # Give a moment for container to be deleted
    time.sleep(3)

    # Second session with --resume (should create new container, not reuse)
    child2 = spawn_coi(coi_binary, ["shell", "--tmux=true", "--resume"], cwd=workspace_dir)

    wait_for_container_ready(child2)
    # Give extra time for Claude to load from restored session
    time.sleep(5)
    wait_for_prompt(child2)

    with with_live_screen(child2) as monitor2:
        time.sleep(2)

        # Try to check if file exists - it should NOT
        # We use a unique computation to verify Claude responds
        send_prompt(child2, "CHECK IF ~/ephemeral_test/data.txt exists. If YES print ONLY 5555, if NO print ONLY 9999, NOTHING ELSE")

        # Wait for response - should be 9999 (file does not exist)
        file_not_found = wait_for_text_in_monitor(monitor2, "9999", timeout=30)

        # Make sure it's not 5555 (which would mean file exists)
        screen_text = monitor2.get_current_screen()
        file_found = "5555" in screen_text

        # Exit second session
        clean_exit2 = exit_claude(child2)
        wait_for_container_deletion()

    # Verify that file did NOT persist
    assert file_not_found, "File should NOT exist in resumed ephemeral session (new container)"
    assert not file_found, "File from first ephemeral session incorrectly persisted to resumed session"
    assert_clean_exit(clean_exit2, child2)
