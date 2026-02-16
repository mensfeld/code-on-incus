"""
Test for power management commands without sudo.

Tests that:
1. shutdown, poweroff, and reboot commands work without sudo prefix
2. Commands don't fail with "Access denied" errors
3. Wrapper scripts are properly configured in /usr/local/bin
"""

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


def test_power_management_no_sudo(coi_binary, cleanup_containers, workspace_dir, dummy_image):
    """
    Test that shutdown/poweroff/reboot work without sudo via wrapper scripts.

    Flow:
    1. Start ephemeral session with dummy CLI
    2. Exit CLI to bash
    3. Verify wrapper scripts exist in /usr/local/bin
    4. Test shutdown --help without sudo
    5. Test poweroff --help without sudo
    6. Test reboot --help without sudo
    7. Verify no "Access denied" or "Permission denied" errors
    8. Cleanup with poweroff (tests wrapper works for actual poweroff)
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Start ephemeral session ===

    child = spawn_coi(
        coi_binary,
        ["shell", "--image", dummy_image],
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

    # Wait for bash prompt to be ready
    time.sleep(3)

    # === Phase 2: Verify wrapper scripts exist ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send(
            "ls -la /usr/local/bin/shutdown /usr/local/bin/poweroff /usr/local/bin/reboot; echo WRAPPERS_CHECK_DONE"
        )
        time.sleep(0.5)
        child.send("\x0d")
        time.sleep(2)

        wrappers_exist = wait_for_text_in_monitor(monitor, "WRAPPERS_CHECK_DONE", timeout=20)

    # === Phase 3: Test shutdown --help without sudo ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("shutdown --help 2>&1 | head -1; echo SHUTDOWN_TEST_DONE")
        time.sleep(0.5)
        child.send("\x0d")
        time.sleep(2)

        shutdown_ok = wait_for_text_in_monitor(monitor, "SHUTDOWN_TEST_DONE", timeout=20)
        access_denied_shutdown = wait_for_text_in_monitor(monitor, "Access denied", timeout=1)
        permission_denied_shutdown = wait_for_text_in_monitor(
            monitor, "Permission denied", timeout=1
        )

    # === Phase 4: Test poweroff --help without sudo ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("poweroff --help 2>&1 | head -1; echo POWEROFF_TEST_DONE")
        time.sleep(0.5)
        child.send("\x0d")
        time.sleep(2)

        poweroff_ok = wait_for_text_in_monitor(monitor, "POWEROFF_TEST_DONE", timeout=20)
        access_denied_poweroff = wait_for_text_in_monitor(monitor, "Access denied", timeout=1)
        permission_denied_poweroff = wait_for_text_in_monitor(
            monitor, "Permission denied", timeout=1
        )

    # === Phase 5: Test reboot --help without sudo ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("reboot --help 2>&1 | head -1; echo REBOOT_TEST_DONE")
        time.sleep(0.5)
        child.send("\x0d")
        time.sleep(2)

        reboot_ok = wait_for_text_in_monitor(monitor, "REBOOT_TEST_DONE", timeout=20)
        access_denied_reboot = wait_for_text_in_monitor(monitor, "Access denied", timeout=1)
        permission_denied_reboot = wait_for_text_in_monitor(monitor, "Permission denied", timeout=1)

    # === Phase 6: Test actual poweroff without sudo (cleanup) ===

    # This also tests that poweroff works without sudo for real cleanup
    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("poweroff")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(2)

        # Should not see "Access denied" error during poweroff
        access_denied_actual_poweroff = wait_for_text_in_monitor(
            monitor, "Access denied", timeout=3
        )

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

    # Assert wrapper scripts exist
    assert wrappers_exist, "Wrapper scripts should exist in /usr/local/bin"

    # Assert all commands worked without permission errors
    assert shutdown_ok, "shutdown --help should complete successfully"
    assert not access_denied_shutdown, "shutdown --help should not show 'Access denied' error"
    assert not permission_denied_shutdown, (
        "shutdown --help should not show 'Permission denied' error"
    )

    assert poweroff_ok, "poweroff --help should complete successfully"
    assert not access_denied_poweroff, "poweroff --help should not show 'Access denied' error"
    assert not permission_denied_poweroff, (
        "poweroff --help should not show 'Permission denied' error"
    )

    assert reboot_ok, "reboot --help should complete successfully"
    assert not access_denied_reboot, "reboot --help should not show 'Access denied' error"
    assert not permission_denied_reboot, "reboot --help should not show 'Permission denied' error"

    assert not access_denied_actual_poweroff, (
        "poweroff should work without sudo (no permission errors)"
    )
