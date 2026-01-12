"""
Test for coi attach - lists multiple sessions.

Tests that:
1. Start two shell sessions in parallel slots
2. Run coi attach with no arguments
3. Verify it lists both sessions instead of attaching
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


def test_attach_lists_multiple_sessions(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi attach lists sessions when multiple are running.

    Flow:
    1. Start coi shell --persistent (slot 1)
    2. Detach
    3. Start coi shell --persistent (slot 2)
    4. Detach
    5. Run coi attach
    6. Verify it lists both sessions
    7. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name_1 = calculate_container_name(workspace_dir, 1)
    container_name_2 = calculate_container_name(workspace_dir, 2)

    # === Phase 1: Start first persistent session ===

    child1 = spawn_coi(
        coi_binary,
        ["shell", "--persistent"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child1, timeout=60)
    wait_for_prompt(child1, timeout=90)

    # Exit claude and bash to detach
    child1.send("exit")
    time.sleep(0.3)
    child1.send("\x0d")
    time.sleep(2)
    child1.send("exit")
    time.sleep(0.3)
    child1.send("\x0d")

    try:
        child1.expect(EOF, timeout=30)
    except TIMEOUT:
        pass

    try:
        child1.close(force=False)
    except Exception:
        child1.close(force=True)

    time.sleep(2)

    # === Phase 2: Start second persistent session ===

    child2 = spawn_coi(
        coi_binary,
        ["shell", "--persistent"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child2, timeout=60)
    wait_for_prompt(child2, timeout=90)

    # Exit claude and bash to detach
    child2.send("exit")
    time.sleep(0.3)
    child2.send("\x0d")
    time.sleep(2)
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

    time.sleep(2)

    # Verify both containers are running
    containers = get_container_list()
    assert container_name_1 in containers, f"Container {container_name_1} should be running"
    assert container_name_2 in containers, f"Container {container_name_2} should be running"

    # === Phase 3: Test coi attach lists sessions ===

    result = subprocess.run(
        [coi_binary, "attach"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed and list sessions
    assert result.returncode == 0, f"coi attach should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should show list header
    assert "Active Claude sessions" in output, (
        f"Should show 'Active Claude sessions'. Got:\n{output}"
    )

    # Should list both containers
    assert container_name_1 in output, f"Should list {container_name_1}. Got:\n{output}"
    assert container_name_2 in output, f"Should list {container_name_2}. Got:\n{output}"

    # Should show usage hint
    assert "coi attach" in output, f"Should show usage hint. Got:\n{output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name_1, "--force"],
        capture_output=True,
        timeout=30,
    )
    subprocess.run(
        [coi_binary, "container", "delete", container_name_2, "--force"],
        capture_output=True,
        timeout=30,
    )

    time.sleep(1)
    containers = get_container_list()
    assert container_name_1 not in containers, f"Container {container_name_1} should be deleted"
    assert container_name_2 not in containers, f"Container {container_name_2} should be deleted"
