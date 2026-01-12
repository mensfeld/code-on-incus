"""
Test for coi shell - UID mapping ensures correct file ownership.

Tests that:
1. Start shell
2. Create a file in /workspace from inside container
3. Verify file has correct ownership on host (not root, not container UID)
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
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_uid_mapping_correct(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that files created inside container have correct UID on host.

    Flow:
    1. Start coi shell
    2. Exit claude to bash
    3. Create a file in /workspace
    4. Exit container
    5. Verify file ownership matches current user (not root, not 1000000+)
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # Get current user's UID
    current_uid = os.getuid()
    os.getgid()

    test_filename = "uid_test_file.txt"
    test_content = "UID_MAPPING_TEST_DATA"

    # === Phase 1: Start session ===

    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    # Exit claude to bash
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # === Phase 2: Create file in /workspace ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send(f"echo '{test_content}' > /workspace/{test_filename}")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        # Verify file was created
        child.send(f"ls -la /workspace/{test_filename}")
        time.sleep(0.3)
        child.send("\x0d")
        file_created = wait_for_text_in_monitor(monitor, test_filename, timeout=10)
        assert file_created, "Should create file in /workspace"

    # === Phase 3: Exit container ===

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

    # Force filesystem sync (important in CI with btrfs)
    subprocess.run(["sync"], check=False)

    # Force cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    # === Phase 4: Verify file ownership on host ===

    file_path = os.path.join(workspace_dir, test_filename)

    # Debug: List all files in workspace
    if not os.path.exists(file_path):
        print(f"\n=== DEBUG: File not found at {file_path} ===")
        print("Workspace dir contents:")
        try:
            for item in os.listdir(workspace_dir):
                item_path = os.path.join(workspace_dir, item)
                stat_info = os.stat(item_path)
                print(
                    f"  {item} (uid={stat_info.st_uid}, gid={stat_info.st_gid}, mode={oct(stat_info.st_mode)})"
                )
        except Exception as e:
            print(f"  Error listing: {e}")
        print(f"Current user: uid={current_uid}, gid={os.getgid()}")
        print("===")

    assert os.path.exists(file_path), f"File {test_filename} should exist on host"

    stat_info = os.stat(file_path)
    file_uid = stat_info.st_uid
    file_gid = stat_info.st_gid

    # File should be owned by current user, NOT by root (0) or high UIDs (1000000+)
    assert file_uid == current_uid, (
        f"File UID should be {current_uid} (current user), got {file_uid}"
    )

    # GID might vary, but should not be root or extremely high
    assert file_gid < 1000000, f"File GID should not be a remapped high UID, got {file_gid}"

    # Cleanup test file
    os.remove(file_path)
