"""
Test that coi shell with [tool] name = "claude" starts the real claude binary.

Verifies that:
1. Writing [tool] name = "claude" to .coi.toml is accepted
2. coi shell starts the container and launches the real claude binary
3. Claude's startup UI appears on screen (login or welcome screen)

No API key is required - Claude displays a login/welcome screen without one.
"""

import os
import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_text_on_screen,
)


def test_claude_tool_starts_session(coi_binary, cleanup_containers, workspace_dir):
    """
    Smoke test: coi shell with tool = "claude" launches the real claude binary.

    Flow:
    1. Write .coi.toml with [tool] name = "claude" to the workspace
    2. Start coi shell (no COI_USE_DUMMY - use the real binary)
    3. Wait for container setup to complete
    4. Wait for Claude's startup UI to appear on screen
    5. Ctrl+C to exit the TUI, then poweroff
    """
    config_path = os.path.join(workspace_dir, ".coi.toml")
    with open(config_path, "w") as f:
        f.write('[tool]\nname = "claude"\n')

    container_name = calculate_container_name(workspace_dir, 1)

    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)

    # Wait for Claude's startup UI - appears regardless of auth state.
    # Without credentials Claude shows a login/welcome screen that contains "Claude".
    claude_started = False
    try:
        wait_for_text_on_screen(child, "Claude", timeout=60)
        claude_started = True
    except Exception:
        pass

    # Stop Claude with Ctrl+C, fall back to bash, then poweroff
    child.send("\x03")
    time.sleep(1)
    child.send("\x03")
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

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    assert claude_started, (
        "coi shell with [tool] name = 'claude' should launch the claude binary "
        "and display its startup UI"
    )
