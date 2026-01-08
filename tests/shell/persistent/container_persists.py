"""
Test that persistent containers are not deleted after session ends.

Flow:
1. Start persistent session on specific slot
2. Exit session
3. Verify container still exists (stopped state)
4. Cleanup

Expected:
- Container exists after session ends
- Container is in stopped state (not running)
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
    exit_claude,
    send_prompt,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_persistent_container_not_deleted(coi_binary, cleanup_containers, workspace_dir):
    """Test that persistent containers are stopped but not deleted."""

    # Start persistent session with specific slot
    child = spawn_coi(
        coi_binary, ["shell", "--tmux=true", "--persistent", "--slot=10"], cwd=workspace_dir
    )

    wait_for_container_ready(child)

    container_name = calculate_container_name(workspace_dir, 10)

    wait_for_prompt(child)

    with with_live_screen(child) as monitor:
        time.sleep(2)
        send_prompt(child, "Print 20 times ONLY result of sum of 100 and 200 and NOTHING ELSE")
        responded = wait_for_text_in_monitor(monitor, "300", timeout=30)
        assert responded, "Claude did not respond"

        # Exit
        time.sleep(1)
        exit_claude(child, timeout=90, use_ctrl_c=True)
        time.sleep(3)

    # Verify container still exists and is stopped
    result = subprocess.run(
        ["sg", "incus-admin", "-c", f"incus list {container_name} --format=csv"],
        capture_output=True,
        text=True,
        shell=False,
    )

    assert container_name in result.stdout, (
        f"Container {container_name} was deleted (persistent mode should keep it)"
    )
    assert "STOPPED" in result.stdout or "Stopped" in result.stdout, "Container should be stopped"

    # Cleanup
    subprocess.run(
        ["sg", "incus-admin", "-c", f"incus delete --force {container_name}"],
        check=False,
        shell=False,
    )
