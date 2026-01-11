"""
Test for coi shell - workspace files persist even in ephemeral mode.

Tests that:
1. Start ephemeral shell
2. Create a file in /workspace
3. Exit and let container be deleted
4. Verify file still exists on host
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
    env = {"COI_USE_TEST_CLAUDE": "1"}
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

    # Wait for cleanup
    time.sleep(5)

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

    import os
    file_path = os.path.join(workspace_dir, test_filename)
    
    assert os.path.exists(file_path), \
        f"File {test_filename} should persist on host after ephemeral container deletion"
    
    with open(file_path, 'r') as f:
        content = f.read().strip()
    
    assert test_content in content, \
        f"File content should be '{test_content}', got '{content}'"

    # Cleanup test file
    os.remove(file_path)
