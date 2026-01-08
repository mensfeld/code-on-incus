"""
Test that persistent containers are reused across sessions.

Flow:
1. Start first persistent session
2. Create a marker file in /tmp (container filesystem)
3. Exit session
4. Start second session with same workspace and slot
5. Verify marker file still exists
6. Cleanup

Expected:
- Same container is reused
- Files in container persist
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


def test_persistent_container_reused_with_state(coi_binary, cleanup_containers, workspace_dir):
    """Test that persistent containers are reused and state persists."""

    # First session - create marker file
    child1 = spawn_coi(
        coi_binary, ["shell", "--tmux=true", "--persistent", "--slot=11"], cwd=workspace_dir
    )

    wait_for_container_ready(child1)

    # Calculate expected container name (deterministic based on workspace + slot)
    container_name = calculate_container_name(workspace_dir, 11)

    wait_for_prompt(child1)

    with with_live_screen(child1) as monitor1:
        time.sleep(2)
        # Test home directory persistence - write specific content
        # Note: /tmp doesn't persist (tmpfs), use absolute path to ensure correct location
        send_prompt(
            child1,
            "mkdir -p ~/persist_test && echo 'persistent-data-12345' > ~/persist_test/data.txt",
        )
        send_prompt(child1, "Print ONLY result of sum of 10000 and 10000 and NOTHING ELSE")
        home_written = wait_for_text_in_monitor(monitor1, "20000", timeout=30)
        assert home_written, "Failed to write test file to /home/claude"

        # Exit
        time.sleep(1)
        exit_claude(child1, timeout=90, use_ctrl_c=True)
        time.sleep(2)

    # Give time for container to fully stop and save state
    time.sleep(5)

    # Verify container still exists (stopped) before second session
    verify_result = subprocess.run(
        ["sg", "incus-admin", "-c", f"incus list {container_name} --format=csv"],
        capture_output=True,
        text=True,
        shell=False,
    )
    assert container_name in verify_result.stdout, (
        f"Container {container_name} was deleted after first session!"
    )

    # Second session - check if marker exists (same workspace, same slot)
    child2 = spawn_coi(
        coi_binary, ["shell", "--tmux=true", "--persistent", "--slot=11"], cwd=workspace_dir
    )

    wait_for_container_ready(child2)

    # Calculate container name again - should be the same as first session
    container_name2 = calculate_container_name(workspace_dir, 11)

    assert container_name == container_name2, (
        f"Container name calculation inconsistent: {container_name} vs {container_name2}"
    )

    wait_for_prompt(child2)

    with with_live_screen(child2) as monitor2:
        time.sleep(2)
        # Check if home directory file persisted with correct content
        send_prompt(
            child2,
            "CHECK IF /home/claude/persist_test/data.txt exists and print ONLY result of 15000+15000 if YES AND NOTHING ELSE",
        )
        home_persisted = wait_for_text_in_monitor(monitor2, "30000", timeout=30)
        assert home_persisted, (
            "Home directory file did not persist - container filesystem not preserved!"
        )

        time.sleep(2)
        exit_claude(child2, timeout=90, use_ctrl_c=True)

    # Final cleanup - delete the persistent container
    subprocess.run(
        ["sg", "incus-admin", "-c", f"incus delete --force {container_name}"],
        check=False,
        shell=False,
    )
