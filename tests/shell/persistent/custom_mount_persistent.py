"""
Test for coi shell --persistent - custom directory mounting in persistent mode.

Tests that:
1. Create a temp directory with a test file
2. Start persistent shell with custom mount
3. Verify the mounted directory and file are accessible inside container
4. Create a new file inside container
5. Verify new file appears on host
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


def test_custom_mount_persistent(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """
    Test that custom directory mounting works in persistent mode.

    Flow:
    1. Create a temp directory with a unique test file
    2. Start coi shell --persistent with the temp dir as workspace
    3. Exit claude to bash
    4. Verify the test file exists in /workspace
    5. Create a new file inside container
    6. Verify new file appears on host
    7. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}

    # === Phase 1: Create temp directory with test file ===

    custom_dir = tmp_path / "custom_mount_persistent_test"
    custom_dir.mkdir()

    test_file = custom_dir / "persistent_mount_test.txt"
    test_content = "PERSISTENT_MOUNT_DATA_67890"
    test_file.write_text(test_content)

    # Verify file exists on host
    assert test_file.exists(), "Test file should exist on host"

    container_name = calculate_container_name(str(custom_dir), 1)

    # === Phase 2: Start persistent shell with custom workspace ===

    child = spawn_coi(
        coi_binary,
        ["shell", "--persistent", f"--workspace={custom_dir}"],
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

    # === Phase 3: Verify mounted file is accessible ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("cat /workspace/persistent_mount_test.txt")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        file_accessible = wait_for_text_in_monitor(monitor, test_content, timeout=10)

    # === Phase 4: Create a new file inside container ===

    new_file_content = "CREATED_INSIDE_CONTAINER_11111"
    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send(f"echo '{new_file_content}' > /workspace/created_inside.txt")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        child.send("cat /workspace/created_inside.txt")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        file_created = wait_for_text_in_monitor(monitor, new_file_content, timeout=10)

    # === Phase 5: Cleanup ===

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

    # Force delete container
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    # === Phase 6: Verify file was created on host ===

    created_file_path = custom_dir / "created_inside.txt"
    file_on_host = created_file_path.exists()
    if file_on_host:
        host_content = created_file_path.read_text().strip()
        content_matches = new_file_content in host_content
    else:
        content_matches = False

    time.sleep(1)
    containers = get_container_list()
    assert container_name not in containers, (
        f"Container {container_name} should be deleted after cleanup"
    )

    # Assert mount worked in both directions
    assert file_accessible, f"Mounted file should be accessible with content '{test_content}'"
    assert file_created, "Should be able to create file inside container"
    assert file_on_host, "File created inside container should appear on host"
    assert content_matches, f"File content on host should match '{new_file_content}'"
