"""
Test that coi shell with [tool] name = "opencode" starts the real opencode binary
and injects the permission bypass into ~/.opencode.json.

Verifies that:
1. Writing [tool] name = "opencode" to .coi.toml is accepted
2. coi shell starts the container and launches the real opencode binary
3. Opencode's startup UI appears on screen (provider/login screen)
4. ~/.opencode.json is created in the container with the permission bypass
   ({"permission": {"*": "allow"}}) via the ToolWithHomeConfigFile injection path

No API key is required - opencode displays a provider selection screen without one.
"""

import json
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


def test_opencode_tool_starts_session(coi_binary, cleanup_containers, workspace_dir):
    """
    Smoke test: coi shell with tool = "opencode" launches the real opencode binary
    and injects the sandbox config into ~/.opencode.json.

    Flow:
    1. Write .coi.toml with [tool] name = "opencode" to the workspace
    2. Start coi shell (no COI_USE_DUMMY - use the real binary)
    3. Wait for container setup to complete
    4. Wait for opencode's startup UI to appear on screen
    5. While TUI is running, use incus exec to inspect ~/.opencode.json
    6. Ctrl+C to exit the TUI, then poweroff
    """
    config_path = os.path.join(workspace_dir, ".coi.toml")
    with open(config_path, "w") as f:
        f.write('[tool]\nname = "opencode"\n')

    container_name = calculate_container_name(workspace_dir, 1)

    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)

    # Wait for opencode's startup UI - appears regardless of auth state.
    # Without API keys opencode shows a provider selection screen containing "opencode".
    opencode_started = False
    try:
        wait_for_text_on_screen(child, "opencode", timeout=60)
        opencode_started = True
    except Exception:
        pass

    # While TUI is running, inspect ~/.opencode.json via incus exec (separate process)
    permission_injected = False
    try:
        result = subprocess.run(
            [
                "incus",
                "exec",
                container_name,
                "--",
                "python3",
                "-c",
                (
                    "import json; "
                    "d = json.load(open('/home/code/.opencode.json')); "
                    "print(json.dumps(d.get('permission', {})))"
                ),
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode == 0:
            perm = json.loads(result.stdout.strip())
            permission_injected = perm.get("*") == "allow"
    except Exception:
        pass

    # Stop opencode with Ctrl+C, fall back to bash, then poweroff
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

    assert opencode_started, (
        "coi shell with [tool] name = 'opencode' should launch the opencode binary "
        "and display its startup UI"
    )
    assert permission_injected, (
        '~/.opencode.json should contain the permission bypass: {"permission": {"*": "allow"}}'
    )
