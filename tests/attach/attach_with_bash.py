"""
Test for coi attach --bash - attach to bash instead of tmux.

Tests that:
1. Start a shell session and detach
2. Run coi attach --bash
3. Verify it attaches to bash shell (not tmux)
"""

import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    get_container_list,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_attach_with_bash(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi attach --bash attaches to bash shell.

    Flow:
    1. Start coi shell --persistent
    2. Detach from session
    3. Run coi attach <name> --bash
    4. Verify we get a bash shell (can run commands)
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

    # Verify container exists
    containers = get_container_list()
    assert container_name in containers, f"Container {container_name} should exist"

    # === Phase 2: Detach ===

    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")

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
    assert container_name in containers, f"Container {container_name} should still be running"

    # === Phase 3: Test coi attach --bash ===

    child2 = spawn_coi(
        coi_binary,
        ["attach", container_name, "--bash"],
        cwd=workspace_dir,
        env=env,
        timeout=60,
    )

    time.sleep(3)

    # Should be in bash - try running a command
    with with_live_screen(child2) as monitor:
        child2.send("echo BASH_TEST_$((100+23))")
        time.sleep(0.3)
        child2.send("\x0d")
        time.sleep(1)
        in_bash = wait_for_text_in_monitor(monitor, "BASH_TEST_123", timeout=10)

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

    # === Phase 4: Cleanup ===

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

    # Assert bash worked
    assert in_bash, "Should be able to run commands in bash shell"
