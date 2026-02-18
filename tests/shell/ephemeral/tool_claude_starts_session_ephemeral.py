"""
Test that coi shell with [tool] name = "claude" starts a session successfully.

Verifies that:
1. Writing [tool] name = "claude" to .coi.toml is accepted
2. coi shell starts the container and invokes the tool binary
3. The tool reaches an interactive prompt (session is live)
"""

import os
import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
)


def test_claude_tool_starts_session(coi_binary, cleanup_containers, workspace_dir):
    """
    Smoke test: coi shell with tool = "claude" starts a session.

    Flow:
    1. Write .coi.toml with [tool] name = "claude" to the workspace
    2. Start coi shell (COI_USE_DUMMY=1 to avoid needing a real API key)
    3. Verify the container starts and the tool reaches an interactive prompt
    4. Cleanup
    """
    # Write a .coi.toml that explicitly selects the claude tool
    config_path = os.path.join(workspace_dir, ".coi.toml")
    with open(config_path, "w") as f:
        f.write('[tool]\nname = "claude"\n')

    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    prompt_reached = False
    try:
        wait_for_prompt(child, timeout=90)
        prompt_reached = True
    except Exception:
        pass

    # Cleanup
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

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    assert prompt_reached, (
        "coi shell with [tool] name = 'claude' should start a session and reach an interactive prompt"
    )
