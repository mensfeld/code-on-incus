"""
Test for coi info - show info for a session.

Tests that:
1. Create a session (by running shell briefly)
2. Run coi info with the session ID
3. Verify output contains expected fields
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


def test_info_with_session(coi_binary, cleanup_containers, workspace_dir):
    """
    Test showing info for a specific session.

    Flow:
    1. Start a shell session and exit
    2. Get the session ID from coi list
    3. Run coi info <session-id>
    4. Verify output contains expected fields
    5. Cleanup
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

    # === Phase 2: Get session ID from coi list ===

    result = subprocess.run(
        [coi_binary, "list", "--all"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    # Find session ID (UUID format)
    uuid_pattern = re.compile(r"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})")
    lines = result.stdout.split("\n")
    session_id = None
    in_sessions_section = False

    for line in lines:
        if "Saved Sessions:" in line:
            in_sessions_section = True
            continue
        if in_sessions_section:
            match = uuid_pattern.search(line)
            if match:
                # Check if this session is for our workspace
                session_id = match.group(1)
                # Read next few lines to check workspace
                break

    if session_id is None:
        # Try to get latest session
        result = subprocess.run(
            [coi_binary, "info"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        # Extract session ID from info output
        match = uuid_pattern.search(result.stdout)
        if match:
            session_id = match.group(1)

    assert session_id is not None, f"Should find a session ID. List output:\n{result.stdout}"

    # === Phase 3: Run coi info with session ID ===

    result = subprocess.run(
        [coi_binary, "info", session_id],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Info should succeed. stderr: {result.stderr}"

    # === Phase 4: Verify output format ===

    output = result.stdout

    assert "Session Information" in output or "Session ID" in output, (
        f"Should show session header. Got:\n{output}"
    )

    assert session_id in output, f"Should show the session ID. Got:\n{output}"

    # Should show resume command
    assert "resume" in output.lower(), f"Should show resume command. Got:\n{output}"

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
