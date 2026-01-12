"""
Test for coi attach --slot with wrong workspace.

Tests that:
1. Start a container in workspace A
2. Try to attach with --slot=1 from workspace B
3. Verify it fails because the container name doesn't match
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


def test_attach_wrong_workspace_slot(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """
    Test that coi attach --slot fails when workspace doesn't match.

    Flow:
    1. Start coi shell --persistent in workspace_dir (slot 1)
    2. Detach
    3. Create a different workspace directory
    4. Try coi attach --slot=1 --workspace=<different>
    5. Verify it fails (different workspace = different container name)
    6. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # Create a different workspace
    other_workspace = tmp_path / "other_workspace"
    other_workspace.mkdir()

    # === Phase 1: Start session in original workspace ===

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

    # === Phase 3: Try to attach with wrong workspace ===

    result = subprocess.run(
        [coi_binary, "attach", "--slot=1", f"--workspace={other_workspace}"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    # Should fail - different workspace means different container name
    combined_output = result.stdout + result.stderr
    attach_failed = (
        result.returncode != 0
        or "not found" in combined_output.lower()
        or "not running" in combined_output.lower()
    )

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    time.sleep(1)
    containers = get_container_list()
    assert container_name not in containers, f"Container {container_name} should be deleted"

    # Assert attach failed
    assert attach_failed, f"Attach with wrong workspace should fail. Got:\n{combined_output}"
