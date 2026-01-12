"""
Test for coi info - verify output contains expected fields.

Tests that:
1. Create a session
2. Run coi info
3. Verify output contains: Session ID, Container, Session Path, Resume command
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


def test_info_output_fields(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi info output contains all expected fields.

    Flow:
    1. Start a shell session and exit
    2. Run coi info
    3. Verify output contains expected fields
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

    # === Phase 2: Run coi info ===

    result = subprocess.run(
        [coi_binary, "info"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Info should succeed. stderr: {result.stderr}"

    output = result.stdout

    # === Phase 3: Verify expected fields ===

    # Session ID field
    assert "Session ID:" in output or "session_id" in output.lower(), (
        f"Should show Session ID field. Got:\n{output}"
    )

    # Container field
    assert "Container:" in output or "container" in output.lower(), (
        f"Should show Container field. Got:\n{output}"
    )

    # Session Path field
    assert "Session Path:" in output or "path" in output.lower(), (
        f"Should show Session Path field. Got:\n{output}"
    )

    # Resume command
    assert "coi shell --resume" in output or "resume" in output.lower(), (
        f"Should show resume command. Got:\n{output}"
    )

    # Session Data status (Present or Missing)
    assert "Session Data:" in output or "data" in output.lower(), (
        f"Should show Session Data status. Got:\n{output}"
    )

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
