"""
Test for coi shell - workspace files persist even in ephemeral mode.

Tests that:
1. Start ephemeral shell
2. Create a file in /workspace
3. Exit and let container be deleted
4. Verify file still exists on host
"""

import os
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


def test_workspace_files_persist_ephemeral(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that files created in /workspace persist after ephemeral container deletion.

    Flow:
    1. Start coi shell (ephemeral)
    2. Exit claude to bash
    3. Create a file in /workspace
    4. Exit (poweroff to trigger container deletion)
    5. Verify file exists on host filesystem
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # Define test file
    test_filename = "ephemeral_persist_test.txt"
    test_content = "EPHEMERAL_WORKSPACE_DATA_54321"

    # === Phase 1: Start ephemeral session ===

    child = spawn_coi(
        coi_binary,
        ["shell"],  # Ephemeral mode (default)
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
        child.send(f"cat /workspace/{test_filename}")
        time.sleep(0.3)
        child.send("\x0d")
        file_created = wait_for_text_in_monitor(monitor, test_content, timeout=10)
        assert file_created, "Should create file in /workspace"

    # === Phase 2.5: DEBUG - Check if file is visible on host while container is running ===
    file_path_check = os.path.join(workspace_dir, test_filename)
    file_exists_on_host = os.path.exists(file_path_check)
    print("\n=== DEBUG BEFORE SHUTDOWN ===")
    print(f"File created in container: {file_created}")
    print(f"File visible on host: {file_exists_on_host}")
    print(f"File path: {file_path_check}")

    # Check container's workspace mount using incus directly
    try:
        result = subprocess.run(
            ["incus", "config", "device", "list", container_name],
            capture_output=True,
            text=True,
            timeout=5,
        )
        print(f"Container devices:\n{result.stdout}")

        # Get full device config for workspace
        result2 = subprocess.run(
            ["incus", "config", "device", "show", container_name],
            capture_output=True,
            text=True,
            timeout=5,
        )
        print(f"\nFull device config:\n{result2.stdout}")

        # Check if files exist inside container
        result3 = subprocess.run(
            ["incus", "exec", container_name, "--", "ls", "-la", "/workspace/"],
            capture_output=True,
            text=True,
            timeout=5,
        )
        print(f"\nFiles in container /workspace:\n{result3.stdout}")

        if result.stderr:
            print(f"Device list stderr: {result.stderr}")
    except Exception as e:
        print(f"Error checking devices: {e}")
    print("=== END DEBUG ===\n")

    # === Phase 3: Poweroff to trigger ephemeral cleanup ===

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

    # Wait for cleanup and filesystem sync
    time.sleep(5)

    # Force filesystem sync (important in CI with btrfs)
    subprocess.run(["sync"], check=False)

    # Verify container is gone (ephemeral mode deletes on poweroff)
    containers = get_container_list()
    # Container might still exist briefly, force delete if needed
    if container_name in containers:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
        time.sleep(2)

    # === Phase 4: Verify file persists on host ===

    file_path = os.path.join(workspace_dir, test_filename)

    # Debug: List all files in workspace and check mount status
    if not os.path.exists(file_path):
        print(f"\n=== DEBUG: File not found at {file_path} ===")
        print(f"Workspace dir exists: {os.path.exists(workspace_dir)}")
        print("Workspace dir contents:")
        try:
            items = os.listdir(workspace_dir)
            if items:
                for item in items:
                    item_path = os.path.join(workspace_dir, item)
                    stat_info = os.stat(item_path)
                    print(
                        f"  {item} (uid={stat_info.st_uid}, gid={stat_info.st_gid}, mode={oct(stat_info.st_mode)})"
                    )
            else:
                print("  (empty directory)")
        except Exception as e:
            print(f"  Error listing: {e}")
        print(f"Current user: uid={os.getuid()}, gid={os.getgid()}")

        # Check if incus device was actually added
        print("\nChecking container device mounts:")
        result = subprocess.run(
            [coi_binary, "container", "show", container_name],
            capture_output=True,
            text=True,
        )
        if result.returncode == 0:
            print(f"Container config:\n{result.stdout}")
        print("===")

    assert os.path.exists(file_path), (
        f"File {test_filename} should persist on host after ephemeral container deletion"
    )

    with open(file_path) as f:
        content = f.read().strip()

    assert test_content in content, f"File content should be '{test_content}', got '{content}'"

    # Cleanup test file
    os.remove(file_path)
