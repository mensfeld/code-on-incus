"""
Test for coi attach - attach by slot.

Tests that:
1. Start a shell session and detach
2. Run coi attach with --slot flag
3. Verify it attaches to the correct session (isolated from other running sessions)
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


def test_auto_attach_single_session(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi attach works with --slot flag (workspace-specific).

    Flow:
    1. Start coi shell --persistent
    2. Detach from session (exit bash, container stays running)
    3. Run coi attach --slot 1
    4. Verify it attaches and shows "Attaching to..."
    5. Cleanup

    Note: This test uses --slot to isolate from other running sessions,
    making it safe to run tests while using coi for development.
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

    # === Phase 2: Detach (exit to bash, then exit bash) ===

    # Exit claude to bash
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # Exit bash (detach - container stays running in persistent mode)
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
    assert container_name in containers, (
        f"Container {container_name} should still be running after detach"
    )

    # === Phase 3: Test coi attach with slot (workspace-specific) ===

    # Run coi attach with --slot to isolate from other running sessions
    # This ensures the test works even when other coi sessions are running
    result = subprocess.run(
        [coi_binary, "attach", "--slot", "1", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=5,
        input="exit\n",  # Exit immediately after attaching
    )

    # Check output mentions attaching
    combined_output = result.stdout + result.stderr
    assert "Attaching to" in combined_output or container_name in combined_output, (
        f"Should show 'Attaching to' message. Got:\n{combined_output}"
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
