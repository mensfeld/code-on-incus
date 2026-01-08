"""
Test file persistence after container exits.

Flow:
1. Start shell
2. Ask Claude to write "IM HERE" to TEST.md
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
    exit_claude,
    send_prompt,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_file_persists_after_container_exit(coi_binary, cleanup_containers, workspace_dir):
    child = spawn_coi(coi_binary, ["shell", "--tmux=true"], cwd=workspace_dir)

    wait_for_container_ready(child)
    wait_for_prompt(child)

    with with_live_screen(child) as monitor:
        time.sleep(2)
        send_prompt(child, 'Write the text "IM HERE" to a file named TEST.md and print first 6 PI characters')
        wait_for_text_in_monitor(monitor, "3.1415", timeout=30)

    clean_exit = exit_claude(child)

    assert clean_exit, "Claude did not exit cleanly"
    assert child.exitstatus == 0, f"Expected exit code 0, got {child.exitstatus}"

    test_file = Path(workspace_dir) / "TEST.md"
    assert test_file.exists(), f"TEST.md was not created in {workspace_dir}"

    content = test_file.read_text()
    assert "IM HERE" in content, f"Expected 'IM HERE' in file, got: {content}"

    test_file.unlink()
