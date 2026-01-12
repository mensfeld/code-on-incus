"""
Test for coi attach - attach to persistent container.

Tests that:
1. Start a persistent shell session
2. Exit and verify container is kept
3. Attach to it
4. Verify attachment works
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


def test_attach_to_persistent(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi attach works with persistent containers.

    Flow:
    1. Start coi shell --persistent
    2. Detach with Ctrl+b d (claude keeps running)
    3. Attach to container
    4. Verify we can still interact with claude
    5. Cleanup
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

    # Quick interaction
    with with_live_screen(child) as monitor:
        time.sleep(2)
        send_prompt(child, "persistent test")
        responded = wait_for_text_in_monitor(monitor, "persistent test-BACK", timeout=30)
        assert responded, "Fake claude should respond"

    # === Phase 2: Detach with Ctrl+b d (container stays running) ===

    # Use tmux detach so claude keeps running
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

    time.sleep(3)

    # Verify container is STILL running (persistent mode)
    containers = get_container_list()
    assert container_name in containers, (
        f"Persistent container {container_name} should still be running after detach"
    )

    # === Phase 3: Attach to persistent container ===

    child2 = spawn_coi(
        coi_binary,
        ["attach", container_name],
        cwd=workspace_dir,
        env=env,
        timeout=60,
    )

    time.sleep(2)

    # We should reconnect to tmux session with claude still running
    # Try interacting with fake-claude again
    with with_live_screen(child2) as monitor:
        time.sleep(2)
        send_prompt(child2, "after attach")
        responded = wait_for_text_in_monitor(monitor, "after attach-BACK", timeout=30)

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

    # Assert attachment worked
    assert responded, "Should be able to interact after attaching to persistent container"
