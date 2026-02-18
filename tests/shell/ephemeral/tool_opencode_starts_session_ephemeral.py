"""
Test that coi shell with [tool] name = "opencode" starts a session successfully
and injects the permission bypass into ~/.opencode.json.

Verifies that:
1. Writing [tool] name = "opencode" to .coi.toml is accepted
2. coi shell starts the container and invokes the tool binary
3. The tool reaches an interactive prompt (session is live)
4. ~/.opencode.json is created in the container with the permission bypass
   ({"permission": {"*": "allow"}}) even when no host config file exists
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
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_opencode_tool_starts_session(coi_binary, cleanup_containers, workspace_dir):
    """
    Smoke test: coi shell with tool = "opencode" starts a session and
    injects the sandbox config into ~/.opencode.json.

    Flow:
    1. Write .coi.toml with [tool] name = "opencode" to the workspace
    2. Start coi shell (COI_USE_DUMMY=1 to avoid needing a real API key)
    3. Verify the container starts and the tool reaches an interactive prompt
    4. Exit CLI to bash
    5. Verify ~/.opencode.json exists with permission bypass injected
    6. Cleanup
    """
    # Write a .coi.toml that selects the opencode tool
    config_path = os.path.join(workspace_dir, ".coi.toml")
    with open(config_path, "w") as f:
        f.write('[tool]\nname = "opencode"\n')

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

    # Exit CLI to bash so we can inspect container state
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(3)

    # Check that ~/.opencode.json was created with the permission bypass
    config_exists = False
    permission_injected = False

    with with_live_screen(child) as monitor:
        time.sleep(1)
        # Test for file existence and permission key in one shot
        child.send(
            "python3 -c \""
            "import json; d=json.load(open('/home/code/.opencode.json'));"
            " print('PERM_OK' if d.get('permission',{}).get('*')=='allow' else 'PERM_MISSING')"
            "\"; echo OPENCODE_CHECK_DONE"
        )
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(3)

        config_exists = wait_for_text_in_monitor(monitor, "OPENCODE_CHECK_DONE", timeout=20)
        permission_injected = wait_for_text_in_monitor(monitor, "PERM_OK", timeout=5)

    # Cleanup
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
        "coi shell with [tool] name = 'opencode' should start a session and reach an interactive prompt"
    )
    assert config_exists, (
        "~/.opencode.json check command should complete (file should exist in container)"
    )
    assert permission_injected, (
        "~/.opencode.json should contain permission bypass: {\"permission\": {\"*\": \"allow\"}}"
    )
