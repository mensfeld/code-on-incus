"""
Test for coi attach --slot - attach by slot number.

Tests that:
1. Start a shell session on slot 1
2. Detach from session
3. Run coi attach --slot=1
4. Verify it attaches to slot 1
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


def test_attach_by_slot(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi attach --slot attaches to the specified slot.

    Flow:
    1. Start coi shell --persistent --slot=1
    2. Detach from session
    3. Run coi attach --slot=1
    4. Verify it attaches and shows "Attaching to ... (slot 1)"
    5. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Start persistent session on slot 1 ===

    child = spawn_coi(
        coi_binary,
        ["shell", "--persistent", "--slot=1"],
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

    # === Phase 3: Test coi attach --slot=1 ===

    result = subprocess.run(
        [coi_binary, "attach", "--slot=1", f"--workspace={workspace_dir}"],
        capture_output=True,
        text=True,
        timeout=5,
        input="exit\n",  # Exit immediately
    )

    # Check output mentions attaching with slot info
    combined_output = result.stdout + result.stderr
    assert "Attaching to" in combined_output or "slot 1" in combined_output.lower(), (
        f"Should show 'Attaching to' or 'slot 1'. Got:\n{combined_output}"
    )

    # Should not show error
    assert "not found" not in combined_output.lower(), (
        f"Should not show 'not found'. Got:\n{combined_output}"
    )

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
