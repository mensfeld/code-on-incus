"""
Test for coi list - shows workspace for containers with session metadata.

Tests that:
1. Start a shell (creates session metadata with workspace)
2. Run coi list while container running
3. Verify it shows Workspace field
"""

import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
)


def test_list_shows_workspace(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list shows workspace for containers.

    Flow:
    1. Start a shell session (creates metadata)
    2. Run coi list while container running
    3. Verify Workspace field appears
    4. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Start shell session ===

    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    time.sleep(2)

    # === Phase 2: Run list while container running ===

    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"

    output = result.stdout

    # === Phase 3: Verify workspace appears ===

    assert container_name in output, f"Container should appear. Got:\n{output}"

    # Should show Workspace field
    assert "Workspace:" in output, f"Should show Workspace field. Got:\n{output}"

    # Workspace should contain part of our workspace path
    assert workspace_dir in output or "workspace" in output.lower(), (
        f"Should show workspace path. Got:\n{output}"
    )

    # === Phase 4: Cleanup ===

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

    time.sleep(3)

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
