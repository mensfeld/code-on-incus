"""
Test that auto_context = false disables context injection into tool-native files.

Verifies that:
1. When auto_context = false in .coi.toml, ~/.claude/CLAUDE.md is NOT created
   (unless the host had one, in which case it's copied but no sandbox context appended).
2. ~/SANDBOX_CONTEXT.md is still created (it's independent of auto_context).
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


def test_auto_context_opt_out_no_claude_md(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """
    Test that setting auto_context = false prevents creation of ~/.claude/CLAUDE.md.

    Flow:
    1. Write .coi.toml with auto_context = false
    2. Start coi shell with dummy (no host ~/.claude/CLAUDE.md)
    3. Exit to bash
    4. Verify ~/.claude/CLAUDE.md does NOT exist in container
    5. Verify ~/SANDBOX_CONTEXT.md still exists (independent feature)
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # Write .coi.toml with auto_context = false
    config_path = os.path.join(workspace_dir, ".coi.toml")
    with open(config_path, "w") as f:
        f.write("[tool]\nauto_context = false\n")

    # Use fake home with .claude dir (credentials needed for config setup)
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    claude_dir = fake_home / ".claude"
    claude_dir.mkdir()
    credentials_file = claude_dir / ".credentials.json"
    credentials_file.write_text('{"token": "test"}')

    env["HOME"] = str(fake_home)

    # Start session
    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    # Exit CLI to bash
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # Check if CLAUDE.md exists in container
    claude_md_result = subprocess.run(
        [
            "sg",
            "incus-admin",
            "-c",
            f"incus exec {container_name} -- test -f /home/code/.claude/CLAUDE.md && echo exists || echo missing",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    claude_md_status = claude_md_result.stdout.strip()

    # Verify SANDBOX_CONTEXT.md still exists (independent of auto_context)
    sandbox_result = subprocess.run(
        [
            "sg",
            "incus-admin",
            "-c",
            f"incus exec {container_name} -- test -f /home/code/SANDBOX_CONTEXT.md && echo exists || echo missing",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    sandbox_status = sandbox_result.stdout.strip()

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

    # Assertions
    assert claude_md_status == "missing", (
        "~/.claude/CLAUDE.md should NOT be created when auto_context = false"
    )

    assert sandbox_status == "exists", (
        "~/SANDBOX_CONTEXT.md should still be created even when auto_context = false "
        "(it's an independent feature)"
    )

    print("✓ auto_context = false correctly disables CLAUDE.md injection")
