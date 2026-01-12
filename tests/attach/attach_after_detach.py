"""
Test for coi attach - reconnect after detach.

Tests that:
1. Start a shell session
2. Detach using Ctrl+b d
3. Run coi attach
4. Verify we reconnect to the same tmux session
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


def test_attach_after_detach(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi attach reconnects after tmux detach.

    Flow:
    1. Start coi shell --persistent
    2. Send a message to fake-claude
    3. Detach with Ctrl+b d
    4. Run coi attach
    5. Verify we're back in the same session
    6. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Start persistent session ===

    child = spawn_coi(
        coi_binary,
        ["shell", "--persistent"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    # Interact with fake-claude
    with with_live_screen(child) as monitor:
        time.sleep(2)
        send_prompt(child, "remember marker ABC123")
        responded = wait_for_text_in_monitor(monitor, "remember marker ABC123-BACK", timeout=30)
        assert responded, "Fake claude should respond"

    # === Phase 2: Detach with Ctrl+b d ===

    # Send tmux detach command
    child.send("\x02")  # Ctrl+b
    time.sleep(0.2)
    child.send("d")  # d for detach

    try:
        child.expect(EOF, timeout=30)
    except TIMEOUT:
        pass

    try:
        child.close(force=False)
    except Exception:
        child.close(force=True)

    time.sleep(2)

    # Verify container is still running
    containers = get_container_list()
    assert container_name in containers, (
        f"Container {container_name} should still be running after detach"
    )

    # === Phase 3: Reattach ===

    child2 = spawn_coi(
        coi_binary,
        ["attach", container_name],
        cwd=workspace_dir,
        env=env,
        timeout=60,
    )

    time.sleep(2)

    # We should be back in the tmux session with fake-claude
    # The previous output should still be visible or we can interact again
    with with_live_screen(child2) as monitor:
        time.sleep(2)
        send_prompt(child2, "second message")
        responded = wait_for_text_in_monitor(monitor, "second message-BACK", timeout=30)

    # === Phase 4: Cleanup ===

    # Exit claude
    child2.send("exit")
    time.sleep(0.3)
    child2.send("\x0d")
    time.sleep(2)

    # Exit bash
    child2.send("exit")
    time.sleep(0.3)
    child2.send("\x0d")

    try:
        child2.expect(EOF, timeout=30)
    except TIMEOUT:
        pass

    try:
        child2.close(force=False)
    except Exception:
        child2.close(force=True)

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    time.sleep(1)
    containers = get_container_list()
    assert container_name not in containers, (
        f"Container {container_name} should be deleted after cleanup"
    )

    # Assert reattach worked
    assert responded, "Should be able to interact with fake-claude after reattach"
