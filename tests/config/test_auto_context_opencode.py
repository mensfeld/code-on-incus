"""
Test auto-context injection into OpenCode's opencode.json instructions field.

Verifies that:
1. When auto_context is enabled (default) and tool is opencode, the
   opencode.json config includes an "instructions" field referencing
   ~/SANDBOX_CONTEXT.md so opencode loads it at session start.
2. The permission bypass config is still present alongside instructions.
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
)


def test_auto_context_opencode_instructions_injected(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that opencode.json includes instructions field with path to
    SANDBOX_CONTEXT.md when auto_context is enabled.

    Flow:
    1. Write .coi.toml with [tool] name = "opencode"
    2. Start coi shell
    3. While running, inspect ~/.config/opencode/opencode.json via incus exec
    4. Verify it contains "instructions" field with SANDBOX_CONTEXT.md path
    5. Verify permission bypass is also present
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

    # Give opencode a moment to start
    time.sleep(5)

    # Inspect opencode.json via incus exec
    instructions_injected = False
    permission_injected = False
    debug_output = ""

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
                    "d = json.load(open('/home/code/.config/opencode/opencode.json')); "
                    "print(json.dumps(d))"
                ),
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode == 0:
            debug_output = result.stdout.strip()
            config = json.loads(debug_output)

            # Check instructions field
            instructions = config.get("instructions", [])
            if isinstance(instructions, list) and len(instructions) > 0:
                for path in instructions:
                    if "SANDBOX_CONTEXT.md" in path:
                        instructions_injected = True
                        break

            # Check permission bypass
            perm = config.get("permission", {})
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

    # Assertions
    assert instructions_injected, (
        f"opencode.json should contain 'instructions' field referencing SANDBOX_CONTEXT.md. "
        f"Got config: {debug_output}"
    )
    assert permission_injected, (
        f"opencode.json should still contain permission bypass. Got config: {debug_output}"
    )
