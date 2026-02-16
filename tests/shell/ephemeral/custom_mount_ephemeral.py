"""
Test for coi shell - custom directory mounting in ephemeral mode.

Tests that:
1. Create a temp directory with a test file
2. Start ephemeral shell with custom mount
3. Verify the mounted directory and file are accessible inside container
"""

import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    get_container_list,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_custom_mount_ephemeral(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """
    Test that custom directory mounting works in ephemeral mode.

    Flow:
    1. Create a temp directory with a unique test file
    2. Start coi shell with the temp dir as workspace
    3. Exit claude to bash
    4. Verify the test file exists in /workspace
    5. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}

    # === Phase 1: Create temp directory with test file ===

    custom_dir = tmp_path / "custom_mount_test"
    custom_dir.mkdir()

    test_file = custom_dir / "mount_test.txt"
    test_content = "CUSTOM_MOUNT_DATA_12345"
    test_file.write_text(test_content)

    # Verify file exists on host
    assert test_file.exists(), "Test file should exist on host"
    assert test_file.read_text() == test_content, "Test file should have correct content"

    container_name = calculate_container_name(str(custom_dir), 1)

    # === Phase 2: Start ephemeral shell with custom workspace ===

    child = spawn_coi(
        coi_binary,
        ["shell", f"--workspace={custom_dir}"],
        cwd=str(custom_dir),
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

    # === Phase 3: Verify mounted file is accessible ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("cat /workspace/mount_test.txt")
        time.sleep(0.5)
        child.send("\x0d")
        time.sleep(2)
        file_accessible = wait_for_text_in_monitor(monitor, test_content, timeout=20)

    # Also verify we can list the directory
    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("ls -la /workspace/")
        time.sleep(0.5)
        child.send("\x0d")
        time.sleep(2)
        dir_listed = wait_for_text_in_monitor(monitor, "mount_test.txt", timeout=20)

    # === Phase 4: Cleanup ===

    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")

    try:
        child.expect(EOF, timeout=30)
    except TIMEOUT:
        pass

    try:
        child.close(force=False)
    except Exception:
        child.close(force=True)

    time.sleep(3)

    # Force delete container if still exists
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    time.sleep(1)
    containers = get_container_list()
    assert container_name not in containers, (
        f"Container {container_name} should be deleted after cleanup"
    )

    # Assert mount worked
    assert file_accessible, f"Mounted file should be accessible with content '{test_content}'"
    assert dir_listed, "Mounted directory should be listable"
