"""
Test that starting without --persistent after --persistent deletes container.

Flow:
1. Start first session with --persistent
2. Exit session (container stays)
3. Start second session WITHOUT --persistent (ephemeral mode, same workspace/slot)
4. Exit session
5. Verify container is deleted

Expected:
- After first session: Container exists (stopped)
- After second session: Container deleted (ephemeral auto-deletes)
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


def test_persistent_to_ephemeral_deletes_container(coi_binary, cleanup_containers, workspace_dir):
    """Test that starting without --persistent after --persistent deletes container."""

    # First session with --persistent
    child1 = spawn_coi(
        coi_binary, ["shell", "--tmux=true", "--persistent", "--slot=21"], cwd=workspace_dir
    )
    wait_for_container_ready(child1)
    container_name = calculate_container_name(workspace_dir, 21)
    wait_for_prompt(child1)

    with with_live_screen(child1) as monitor1:
        time.sleep(2)
        send_prompt(child1, "Print ONLY result of sum of 2000 and 3000 and NOTHING ELSE")
        responded = wait_for_text_in_monitor(monitor1, "5000", timeout=30)
        assert responded, "Claude did not respond in first session"

        # Exit first session
        time.sleep(1)
        exit_claude(child1, timeout=90, use_ctrl_c=True)
        time.sleep(3)

    # Verify container exists and is stopped
    result = subprocess.run(
        ["sg", "incus-admin", "-c", f"incus list {container_name} --format=csv"],
        capture_output=True,
        text=True,
        shell=False,
    )
    assert container_name in result.stdout, (
        f"Container {container_name} should exist after persistent session"
    )

    # Second session WITHOUT --persistent (ephemeral mode)
    child2 = spawn_coi(coi_binary, ["shell", "--tmux=true", "--slot=21"], cwd=workspace_dir)
    wait_for_container_ready(child2)
    wait_for_prompt(child2)

    with with_live_screen(child2) as monitor2:
        time.sleep(2)
        send_prompt(child2, "Print ONLY result of sum of 4000 and 6000 and NOTHING ELSE")
        responded = wait_for_text_in_monitor(monitor2, "10000", timeout=30)
        assert responded, "Claude did not respond in second session"

        # Exit second session (ephemeral, so container should be deleted)
        time.sleep(1)
        exit_claude(child2, timeout=90, use_ctrl_c=True)
        time.sleep(3)

    # Verify container is deleted (ephemeral containers auto-delete)
    result = subprocess.run(
        ["sg", "incus-admin", "-c", f"incus list {container_name} --format=csv"],
        capture_output=True,
        text=True,
        shell=False,
    )
    assert container_name not in result.stdout, (
        f"Ephemeral container {container_name} should be deleted"
    )
