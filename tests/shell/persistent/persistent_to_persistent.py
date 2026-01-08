"""
Test that persistent->persistent reuses the same container.

Flow:
1. Start first persistent session
2. Exit session
3. Start second persistent session (same workspace/slot)
4. Verify same container UUID (container was reused, not recreated)
5. Cleanup

Expected:
- Container UUID matches between sessions
- Container is reused, not recreated
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


def test_persistent_to_persistent_reuses_container(coi_binary, cleanup_containers, workspace_dir):
    """Test that persistent->persistent reuses the same container."""

    # First session with --persistent
    child1 = spawn_coi(
        coi_binary, ["shell", "--tmux=true", "--persistent", "--slot=20"], cwd=workspace_dir
    )
    wait_for_container_ready(child1)
    container_name = calculate_container_name(workspace_dir, 20)
    wait_for_prompt(child1)

    with with_live_screen(child1) as monitor1:
        time.sleep(2)
        send_prompt(child1, "Print ONLY result of sum of 400 and 600 and NOTHING ELSE")
        responded = wait_for_text_in_monitor(monitor1, "1000", timeout=30)
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
    assert container_name in result.stdout, f"Container {container_name} should exist"
    assert "STOPPED" in result.stdout, "Container should be stopped"

    # Get UUID before second session
    uuid_before = subprocess.run(
        ["sg", "incus-admin", "-c", f"incus config get {container_name} volatile.uuid"],
        capture_output=True,
        text=True,
        shell=False,
    )

    # Second session with --persistent (same workspace/slot)
    child2 = spawn_coi(
        coi_binary, ["shell", "--tmux=true", "--persistent", "--slot=20"], cwd=workspace_dir
    )
    wait_for_container_ready(child2)
    wait_for_prompt(child2)

    # Get UUID after second session
    uuid_after = subprocess.run(
        ["sg", "incus-admin", "-c", f"incus config get {container_name} volatile.uuid"],
        capture_output=True,
        text=True,
        shell=False,
    )

    # UUIDs should match (same container reused)
    assert uuid_before.stdout.strip() == uuid_after.stdout.strip(), (
        "Container should be reused (UUID should match)"
    )

    with with_live_screen(child2) as monitor2:
        time.sleep(2)
        send_prompt(child2, "Print ONLY result of sum of 700 and 800 and NOTHING ELSE")
        responded = wait_for_text_in_monitor(monitor2, "1500", timeout=30)
        assert responded, "Claude did not respond in second session"

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
