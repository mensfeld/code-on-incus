"""
Test for coi list --all - shows session details.

Tests that:
1. Create a session
2. Run coi list --all
3. Verify session appears with ID, saved time, workspace
"""

import re
import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
)


def test_list_all_with_session(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list --all shows saved session details.

    Flow:
    1. Start a shell session and exit (saves session)
    2. Run coi list --all
    3. Verify session appears with details
    4. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

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

    # Exit via poweroff to save session
    time.sleep(2)
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

    # === Phase 2: Run list --all ===

    result = subprocess.run(
        [coi_binary, "list", "--all"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List --all should succeed. stderr: {result.stderr}"

    output = result.stdout

    # === Phase 3: Verify session details ===

    # Should show Saved Sessions section
    assert "Saved Sessions:" in output, f"Should show Saved Sessions section. Got:\n{output}"

    # Should show a UUID (session ID)
    uuid_pattern = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
    assert uuid_pattern.search(output), f"Should contain a session UUID. Got:\n{output}"

    # Should show Saved timestamp
    assert "Saved:" in output, f"Should show Saved field. Got:\n{output}"

    # Should show Workspace
    assert "Workspace:" in output, f"Should show Workspace field. Got:\n{output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
