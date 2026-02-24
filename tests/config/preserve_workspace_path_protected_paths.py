"""
Test for preserve_workspace_path with protected paths.

Tests that:
1. When preserve_workspace_path is enabled, protected paths (.git/hooks) are
   mounted read-only at the correct location (not /workspace/.git/hooks but
   at the preserved path)
2. This verifies the critical bug fix where SetupSecurityMounts was hardcoded
   to use /workspace instead of the dynamic container workspace path
"""

import os
import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_protected_paths_work_with_preserve_workspace_path(
    coi_binary, cleanup_containers, workspace_dir
):
    """
    Test that protected paths (.git/hooks) are read-only when preserve_workspace_path is enabled.

    This test verifies the fix for the bug where SetupSecurityMounts hardcoded
    /workspace as the container path, causing protected paths to be mounted at
    the wrong location when preserve_workspace_path was enabled.

    Flow:
    1. Create a git repo in workspace with a hook file
    2. Enable preserve_workspace_path
    3. Start coi shell
    4. Try to modify .git/hooks from inside container
    5. Verify modification is blocked (read-only)
    """
    env = {"COI_USE_DUMMY": "1"}

    # Create .coi.toml with preserve_workspace_path enabled
    config_path = os.path.join(workspace_dir, ".coi.toml")
    with open(config_path, "w") as f:
        f.write(
            """
[paths]
preserve_workspace_path = true
"""
        )

    # Initialize a git repo and create a hook
    subprocess.run(["git", "init"], cwd=workspace_dir, capture_output=True, check=True)
    hooks_dir = os.path.join(workspace_dir, ".git", "hooks")
    os.makedirs(hooks_dir, exist_ok=True)
    hook_file = os.path.join(hooks_dir, "pre-commit")
    with open(hook_file, "w") as f:
        f.write("#!/bin/bash\necho 'Original hook'\n")
    os.chmod(hook_file, 0o755)

    # Start shell
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

    # Try to write to .git/hooks - should fail because it's read-only
    # The hook should be at {workspace_dir}/.git/hooks/pre-commit (preserved path)
    # not at /workspace/.git/hooks/pre-commit
    with with_live_screen(child) as monitor:
        time.sleep(1)

        # First verify we're at the correct path
        child.send("pwd")
        time.sleep(0.3)
        child.send("\x0d")
        found = wait_for_text_in_monitor(monitor, workspace_dir, timeout=10)
        assert found, f"Should be at {workspace_dir}"

        # Verify the hook file exists at the preserved path
        child.send(f"cat {workspace_dir}/.git/hooks/pre-commit")
        time.sleep(0.3)
        child.send("\x0d")
        found = wait_for_text_in_monitor(monitor, "Original hook", timeout=10)
        assert found, "Hook file should exist and be readable at preserved path"

        # Try to modify the hook - should fail with read-only error
        child.send(f"echo 'Modified' >> {workspace_dir}/.git/hooks/pre-commit 2>&1")
        time.sleep(0.3)
        child.send("\x0d")
        # Look for "Read-only" error message
        found = wait_for_text_in_monitor(monitor, "Read-only", timeout=10)
        assert found, "Writing to .git/hooks should fail with Read-only error"

    # Verify the hook was NOT modified on host
    with open(hook_file) as f:
        content = f.read()
    assert "Modified" not in content, "Hook should not have been modified"
    assert "Original hook" in content, "Hook should still have original content"

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


def test_protected_paths_default_workspace(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that protected paths work correctly with default /workspace path.

    This is a baseline test to ensure the fix doesn't break the default behavior.
    """
    env = {"COI_USE_DUMMY": "1"}

    # Initialize a git repo and create a hook
    subprocess.run(["git", "init"], cwd=workspace_dir, capture_output=True, check=True)
    hooks_dir = os.path.join(workspace_dir, ".git", "hooks")
    os.makedirs(hooks_dir, exist_ok=True)
    hook_file = os.path.join(hooks_dir, "pre-commit")
    with open(hook_file, "w") as f:
        f.write("#!/bin/bash\necho 'Original hook'\n")
    os.chmod(hook_file, 0o755)

    # Start shell (default config, no preserve_workspace_path)
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

    # Try to write to .git/hooks at /workspace - should fail
    with with_live_screen(child) as monitor:
        time.sleep(1)

        # Verify we're at /workspace
        child.send("pwd")
        time.sleep(0.3)
        child.send("\x0d")
        found = wait_for_text_in_monitor(monitor, "/workspace", timeout=10)
        assert found, "Should be at /workspace"

        # Verify the hook file exists
        child.send("cat /workspace/.git/hooks/pre-commit")
        time.sleep(0.3)
        child.send("\x0d")
        found = wait_for_text_in_monitor(monitor, "Original hook", timeout=10)
        assert found, "Hook file should exist and be readable"

        # Try to modify the hook - should fail with read-only error
        child.send("echo 'Modified' >> /workspace/.git/hooks/pre-commit 2>&1")
        time.sleep(0.3)
        child.send("\x0d")
        found = wait_for_text_in_monitor(monitor, "Read-only", timeout=10)
        assert found, "Writing to .git/hooks should fail with Read-only error"

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
