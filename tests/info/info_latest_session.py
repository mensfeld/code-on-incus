"""
Test for coi info - show info for latest session (no args).

Tests that:
1. Create a session
2. Run coi info without arguments
3. Verify it shows the latest session
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


def test_info_latest_session(coi_binary, cleanup_containers, workspace_dir):
    """
    Test showing info for the latest session (no session ID provided).

    Flow:
    1. Start a shell session and exit
    2. Run coi info (no args)
    3. Verify it shows session information
    4. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Start and stop a session ===

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

    # === Phase 2: Run coi info without arguments ===

    result = subprocess.run(
        [coi_binary, "info"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed (we just created a session)
    assert result.returncode == 0, (
        f"Info should succeed after creating session. stderr: {result.stderr}"
    )

    # === Phase 3: Verify output ===

    output = result.stdout

    assert "Session Information" in output or "Session ID" in output, (
        f"Should show session header. Got:\n{output}"
    )

    # Should contain a UUID
    uuid_pattern = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
    assert uuid_pattern.search(output), f"Should contain a session UUID. Got:\n{output}"

    # Should show resume command
    assert "resume" in output.lower(), f"Should show resume command. Got:\n{output}"

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
