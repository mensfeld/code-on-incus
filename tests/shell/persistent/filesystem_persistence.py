"""
Test that filesystem changes persist across container restarts.

Flow:
1. Start first persistent session
2. Create test file in /home/claude
3. Exit session (container stops)
4. Start second persistent session (container restarts)
5. Verify test file still exists with correct content
6. Cleanup

Expected:
- Files created in /home/claude survive container stop/start
- Filesystem state is preserved in persistent containers
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    exit_claude,
    send_prompt,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_filesystem_persistence_across_restarts(coi_binary, cleanup_containers, workspace_dir):
    """Test that filesystem changes persist across container restarts."""

    # First session with --persistent
    child1 = spawn_coi(
        coi_binary, ["shell", "--tmux=true", "--persistent", "--slot=23"], cwd=workspace_dir
    )
    wait_for_container_ready(child1)
    container_name = calculate_container_name(workspace_dir, 23)
    wait_for_prompt(child1)

    with with_live_screen(child1) as monitor1:
        time.sleep(2)

        # Create test file in home directory
        send_prompt(child1, "mkdir -p ~/test_fs && echo 'fs-test-987' > ~/test_fs/data.txt")
        send_prompt(child1, "Print ONLY result of sum of 11000 and 13000 and NOTHING ELSE")
        file_created = wait_for_text_in_monitor(monitor1, "24000", timeout=30)
        assert file_created, "Failed to create test file"

        # Exit first session
        time.sleep(1)
        exit_claude(child1, timeout=90, use_ctrl_c=True)
        time.sleep(3)

    # Wait for container to stop
    time.sleep(2)

    # Second session with --persistent (restart container)
    child2 = spawn_coi(
        coi_binary, ["shell", "--tmux=true", "--persistent", "--slot=23"], cwd=workspace_dir
    )
    wait_for_container_ready(child2)
    wait_for_prompt(child2)

    with with_live_screen(child2) as monitor2:
        time.sleep(2)

        # Verify file exists via Claude
        send_prompt(
            child2,
            "CHECK IF ~/test_fs/data.txt exists and print ONLY result of 16000+18000 if YES AND NOTHING ELSE",
        )
        file_persisted = wait_for_text_in_monitor(monitor2, "34000", timeout=30)
        assert file_persisted, "File should persist across container restarts"

        # Exit second session
        time.sleep(1)
        exit_claude(child2, timeout=90, use_ctrl_c=True)
        time.sleep(3)

    # Cleanup
    subprocess.run(
        ["sg", "incus-admin", "-c", f"incus delete --force {container_name}"],
        check=False,
        shell=False,
    )
