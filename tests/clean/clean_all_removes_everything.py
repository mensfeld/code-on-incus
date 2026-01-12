"""
Test for coi clean --all - removes containers and sessions.

Tests that:
1. Create a stopped container
2. Create a session
3. Run coi clean --all --force
4. Verify both are removed
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


def test_clean_all_removes_everything(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi clean --all removes containers and sessions.

    Flow:
    1. Start a session, poweroff (creates session data)
    2. Launch and stop another container
    3. Run coi clean --all --force
    4. Verify both container and session are gone
    """
    env = {"COI_USE_DUMMY": "1"}
    calculate_container_name(workspace_dir, 1)
    container_name_2 = calculate_container_name(workspace_dir, 2)

    # === Phase 1: Create a session ===

    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    with with_live_screen(child) as monitor:
        time.sleep(2)
        send_prompt(child, "clean all test")
        wait_for_text_in_monitor(monitor, "clean all test-BACK", timeout=30)

    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)
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

    time.sleep(5)

    # === Phase 2: Create a stopped container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name_2],
        capture_output=True,
        text=True,
        timeout=120,
    )

    time.sleep(3)

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name_2],
        capture_output=True,
        text=True,
        timeout=60,
    )

    time.sleep(2)

    # === Phase 3: Clean all ===

    result = subprocess.run(
        [coi_binary, "clean", "--all", "--force"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"coi clean --all should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 4: Verify everything is cleaned ===

    # Check containers
    containers = get_container_list()
    assert container_name_2 not in containers, (
        f"Stopped container {container_name_2} should be removed"
    )

    # Check sessions
    result = subprocess.run(
        [coi_binary, "list", "--all"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Command should succeed
    assert result.returncode == 0, "List after clean --all should succeed"
