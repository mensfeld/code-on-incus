"""
Test for preserve_workspace_path config option.

Tests that:
1. When preserve_workspace_path is enabled, workspace is mounted at host path
2. When disabled (default), workspace is mounted at /workspace
"""

import os
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_preserve_workspace_path_enabled(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that when preserve_workspace_path is enabled, workspace is mounted at host path.

    Flow:
    1. Create config with preserve_workspace_path = true
    2. Start coi shell
    3. Check that workspace is mounted at host path
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

    # Check that workspace is mounted at host path
    with with_live_screen(child) as monitor:
        time.sleep(1)
        # pwd should show the host path, not /workspace
        child.send("pwd")
        time.sleep(0.3)
        child.send("\x0d")

        # The workspace_dir is the host path, so it should appear
        # Note: workspace_dir is an absolute path like /tmp/pytest-xxx/workspace
        found = wait_for_text_in_monitor(monitor, workspace_dir, timeout=10)
        assert found, f"Workspace should be mounted at {workspace_dir}, not /workspace"

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


def test_preserve_workspace_path_disabled_default(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that by default (preserve_workspace_path not set), workspace is mounted at /workspace.

    Flow:
    1. Start coi shell without preserve_workspace_path config
    2. Check that workspace is mounted at /workspace
    """
    env = {"COI_USE_DUMMY": "1"}

    # Start shell (no special config)
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

    # Check that workspace is mounted at /workspace
    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("pwd")
        time.sleep(0.3)
        child.send("\x0d")

        # Should be /workspace
        found = wait_for_text_in_monitor(monitor, "/workspace", timeout=10)
        assert found, "Workspace should be mounted at /workspace by default"

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
