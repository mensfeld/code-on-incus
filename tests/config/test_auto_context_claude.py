"""
Test auto-context injection into Claude Code's ~/.claude/CLAUDE.md.

Verifies that:
1. When auto_context is enabled (default), sandbox context is written to
   ~/.claude/CLAUDE.md so Claude Code auto-loads it at session start.
2. When a host CLAUDE.md already exists, the sandbox context is appended
   (not overwritten) with a separator, preserving user instructions.
3. CLAUDE.md is included in EssentialConfigFiles so the host file is copied
   into the container before sandbox context is appended.
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


def test_auto_context_claude_creates_claude_md(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """
    Test that ~/.claude/CLAUDE.md is created with sandbox context when no
    host CLAUDE.md exists.

    Flow:
    1. Start coi shell with dummy (no host ~/.claude/CLAUDE.md)
    2. Exit to bash
    3. Read ~/.claude/CLAUDE.md from container
    4. Verify it contains sandbox context markers
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # Use a fake home without CLAUDE.md (but with .claude dir for credentials)
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    claude_dir = fake_home / ".claude"
    claude_dir.mkdir()

    # Create minimal credentials so Claude config setup runs
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

    # Read CLAUDE.md from container
    result = subprocess.run(
        [
            "sg",
            "incus-admin",
            "-c",
            f"incus exec {container_name} -- cat /home/code/.claude/CLAUDE.md",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    claude_md_content = result.stdout
    claude_md_exists = result.returncode == 0

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
    assert claude_md_exists, (
        "~/.claude/CLAUDE.md should be created when auto_context is enabled (default)"
    )

    assert "COI Sandbox" in claude_md_content, (
        f"CLAUDE.md should contain sandbox context. Got:\n{claude_md_content[:500]}"
    )


def test_auto_context_claude_preserves_host_claude_md(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """
    Test that when host has ~/.claude/CLAUDE.md, the sandbox context is
    appended (not overwritten) and the host content is preserved.

    Flow:
    1. Create fake home with ~/.claude/CLAUDE.md containing user instructions
    2. Start coi shell with dummy
    3. Exit to bash
    4. Read ~/.claude/CLAUDE.md from container
    5. Verify it contains BOTH user instructions AND sandbox context
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # Create fake home with CLAUDE.md
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    claude_dir = fake_home / ".claude"
    claude_dir.mkdir()

    # Create host CLAUDE.md with user instructions
    host_instructions = (
        "# My Custom Instructions\n\nAlways use TypeScript.\nPrefer functional style.\n"
    )
    claude_md_file = claude_dir / "CLAUDE.md"
    claude_md_file.write_text(host_instructions)

    # Create minimal credentials so config setup runs
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

    # Read CLAUDE.md from container
    result = subprocess.run(
        [
            "sg",
            "incus-admin",
            "-c",
            f"incus exec {container_name} -- cat /home/code/.claude/CLAUDE.md",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    claude_md_content = result.stdout

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
    assert result.returncode == 0, (
        f"Failed to read CLAUDE.md from container. stderr: {result.stderr}"
    )

    # Host content should be preserved (appears first)
    assert "My Custom Instructions" in claude_md_content, (
        f"Host CLAUDE.md content should be preserved. Got:\n{claude_md_content[:500]}"
    )
    assert "Always use TypeScript" in claude_md_content, (
        f"Host CLAUDE.md instructions should be preserved. Got:\n{claude_md_content[:500]}"
    )

    # Sandbox context should be appended
    assert "COI Sandbox Context" in claude_md_content, (
        f"Sandbox context separator should be present. Got:\n{claude_md_content[:500]}"
    )
    assert "COI Sandbox" in claude_md_content, (
        f"Sandbox context content should be appended. Got:\n{claude_md_content[:500]}"
    )

    # Verify order: host content comes before sandbox context
    host_pos = claude_md_content.index("My Custom Instructions")
    sandbox_pos = claude_md_content.index("COI Sandbox Context")
    assert host_pos < sandbox_pos, (
        "Host CLAUDE.md content should appear BEFORE sandbox context "
        "(sandbox context should be appended, not prepended)"
    )

    print("✓ Host CLAUDE.md preserved and sandbox context appended")
