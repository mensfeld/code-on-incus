"""
Test for coi list - persistent containers should show as persistent.

Tests that:
1. Start a persistent session with fake-claude
2. Run coi list
3. Verify container IS marked as (persistent)
4. Cleanup container
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


def test_list_persistent(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that persistent containers are marked as persistent in coi list.

    Flow:
    1. Start coi shell --persistent
    2. Verify container is running
    3. Run coi list and verify container IS marked as (persistent)
    4. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}

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

    container_name = calculate_container_name(workspace_dir, 1)

    # Verify container exists
    containers = get_container_list()
    assert container_name in containers, f"Container {container_name} should exist"

    # Interact briefly with fake-claude to ensure session is established
    with with_live_screen(child) as monitor:
        time.sleep(2)
        send_prompt(child, "test message")
        responded = wait_for_text_in_monitor(monitor, "test message-BACK", timeout=30)
        assert responded, "Fake claude should respond"

    # === Phase 2: Run coi list and check output ===

    list_result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert list_result.returncode == 0, f"coi list should succeed. stderr: {list_result.stderr}"

    list_output = list_result.stdout

    # Container should be listed
    assert container_name in list_output, (
        f"Container {container_name} should be in list output. Got:\n{list_output}"
    )

    # Container SHOULD be marked as persistent
    lines = list_output.split("\n")
    container_line = None
    for line in lines:
        if container_name in line:
            container_line = line
            break

    assert container_line is not None, f"Should find container {container_name} in output"

    assert "(persistent)" in container_line, (
        f"Persistent container should be marked as (persistent). Line: {container_line}"
    )

    assert "(ephemeral)" not in container_line, (
        f"Persistent container should NOT be marked as ephemeral. Line: {container_line}"
    )

    # === Phase 3: Cleanup ===

    # Exit claude
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # Exit bash
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

    # Force delete the container
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    # Verify container is gone
    time.sleep(1)
    containers = get_container_list()
    assert container_name not in containers, (
        f"Container {container_name} should be deleted after cleanup"
    )
