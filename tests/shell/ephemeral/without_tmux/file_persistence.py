"""
Test file persistence after container exits.

Flow:
1. Start shell
2. Ask Claude to write "IM HERE" to HELLO.md
3. Exit container
4. Verify file exists in workspace with correct content
5. Clean up file

Expected:
- Files created in container persist in workspace after exit
- File content is correct
"""

import time
from pathlib import Path

from support.helpers import (
    assert_clean_exit,
    exit_claude,
    send_prompt,
    spawn_coi,
    wait_for_container_deletion,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_file_persists_after_container_exit(coi_binary, cleanup_containers, workspace_dir):
    child = spawn_coi(coi_binary, ["shell", "--tmux=false"], cwd=workspace_dir)

    wait_for_container_ready(child)
    wait_for_prompt(child)

    with with_live_screen(child) as monitor:
        time.sleep(2)
        send_prompt(
            child,
            'Write the text "IM HERE" to a file named HELLO.md and after that print first 6 PI digits',
        )
        wait_for_text_in_monitor(monitor, "14159", timeout=30)

        # Exit Claude and wait for container cleanup while monitor is still running
        clean_exit = exit_claude(child)
        wait_for_container_deletion()  # Wait for Incus cleanup to complete

    # Now assert after monitor has stopped
    assert_clean_exit(clean_exit, child)

    test_file = Path(workspace_dir) / "HELLO.md"
    assert test_file.exists(), f"HELLO.md was not created in {workspace_dir}"

    content = test_file.read_text()
    assert "IM HERE" in content, f"Expected 'IM HERE' in file, got: {content}"

    test_file.unlink()
