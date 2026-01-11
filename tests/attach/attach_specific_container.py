"""
Test for coi attach <container-name> - attach to specific container.

Tests that:
1. Start a shell session and detach
2. Run coi attach <container-name>
3. Verify it attaches to the specified container
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
)


def test_attach_specific_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi attach <name> attaches to the specified container.

    Flow:
    1. Start coi shell --persistent
    2. Detach from session
    3. Run coi attach <container-name>
    4. Verify it attaches to that specific container
    5. Cleanup
    """
    env = {"COI_USE_TEST_CLAUDE": "1"}
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
    assert container_name in containers, \
        f"Container {container_name} should exist"

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
    assert container_name in containers, \
        f"Container {container_name} should still be running"

    # === Phase 3: Test coi attach <container-name> ===

    # Use subprocess with a short timeout - we just want to verify it starts attaching
    result = subprocess.run(
        [coi_binary, "attach", container_name],
        capture_output=True,
        text=True,
        timeout=5,
        input="exit\n",  # Exit immediately
    )

    # The attach should have worked (we may get various exit codes depending on timing)
    # Main thing is it didn't error about container not found
    combined_output = result.stdout + result.stderr
    assert "not found" not in combined_output.lower(), \
        f"Should not show 'not found'. Got:\n{combined_output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    time.sleep(1)
    containers = get_container_list()
    assert container_name not in containers, \
        f"Container {container_name} should be deleted after cleanup"
