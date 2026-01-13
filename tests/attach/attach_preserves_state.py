"""
Test for coi attach - state is preserved after detach/attach.

Tests that:
1. Start a shell session
2. Create a file in the container
3. Detach
4. Attach again
5. Verify the file still exists
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


def test_attach_preserves_state(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that container state is preserved after detach/attach.

    Flow:
    1. Start coi shell --persistent
    2. Exit claude to bash, create a file
    3. Detach with Ctrl+b d
    4. Attach again with --bash
    5. Verify the file still exists
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

    # Exit CLI to bash
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # === Phase 2: Create a unique file ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("echo 'STATE_PRESERVED_DATA_789' > ~/state_test.txt")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        child.send("cat ~/state_test.txt")
        time.sleep(0.3)
        child.send("\x0d")
        created = wait_for_text_in_monitor(monitor, "STATE_PRESERVED_DATA_789", timeout=10)
        assert created, "Should create state test file"

    # === Phase 3: Detach with Ctrl+b d ===

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
    assert container_name in containers, f"Container {container_name} should still be running"

    # === Phase 4: Attach with --bash and verify file ===

    child2 = spawn_coi(
        coi_binary,
        ["attach", container_name, "--bash"],
        cwd=workspace_dir,
        env=env,
        timeout=60,
    )

    time.sleep(3)

    # Check if file still exists
    with with_live_screen(child2) as monitor:
        time.sleep(1)
        child2.send("cat ~/state_test.txt")
        time.sleep(0.3)
        child2.send("\x0d")
        time.sleep(1)
        file_exists = wait_for_text_in_monitor(monitor, "STATE_PRESERVED_DATA_789", timeout=10)

    # === Phase 5: Cleanup ===

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

    # Assert state was preserved
    assert file_exists, "File should still exist after detach/attach (state preserved)"
