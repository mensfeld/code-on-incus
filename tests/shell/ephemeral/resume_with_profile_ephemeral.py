"""
Test for coi shell --resume with profile auto-restore (#342).

Tests that:
1. Create a profile with a distinguishable setting (persistent = true)
2. Start session with --profile testprofile
3. Poweroff to save session
4. Resume with --resume (no --profile)
5. Verify output contains "Inherited profile 'testprofile' from session"
6. Verify inherited persistent mode from the profile
"""

import os
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_on_screen,
)


def test_resume_with_profile(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that --resume automatically restores the profile from the original session.

    Flow:
    1. Create a profile directory with persistent = true
    2. Start session with --profile testprofile
    3. Poweroff to save session (with profile in metadata)
    4. Resume with --resume (no --profile flag)
    5. Verify "Inherited profile 'testprofile' from session" appears
    6. Verify "Inherited persistent mode from session" appears (from profile config)
    7. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Create profile with persistent = true ===
    profile_dir = os.path.join(workspace_dir, ".coi", "profiles", "testprofile")
    os.makedirs(profile_dir, exist_ok=True)

    config_path = os.path.join(profile_dir, "config.toml")
    with open(config_path, "w") as f:
        f.write("[container]\npersistent = true\n")

    # === Phase 2: Start session with --profile testprofile ===

    child1 = spawn_coi(
        coi_binary,
        ["shell", "--profile", "testprofile"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child1, timeout=60)
    wait_for_prompt(child1, timeout=90)

    # Exit dummy, then poweroff to save session
    child1.send("exit")
    time.sleep(0.3)
    child1.send("\x0d")
    time.sleep(2)
    child1.send("sudo poweroff")
    time.sleep(0.3)
    child1.send("\x0d")

    try:
        child1.expect(EOF, timeout=60)
    except TIMEOUT:
        pass

    try:
        child1.close(force=False)
    except Exception:
        child1.close(force=True)

    time.sleep(5)

    # === Phase 3: Resume with --resume (no --profile) ===

    child2 = spawn_coi(
        coi_binary,
        ["shell", "--resume"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    # Check for profile inheritance message
    try:
        wait_for_text_on_screen(child2, "Inherited profile 'testprofile' from session", timeout=30)
        profile_inherited = True
    except TimeoutError:
        profile_inherited = False

    # Check for persistent mode inheritance (comes from the profile's persistent = true)
    try:
        wait_for_text_on_screen(child2, "Inherited persistent mode from session", timeout=10)
        persistent_inherited = True
    except TimeoutError:
        persistent_inherited = False

    wait_for_container_ready(child2, timeout=60)

    # === Phase 4: Cleanup ===

    child2.send("exit")
    time.sleep(0.3)
    child2.send("\x0d")
    time.sleep(2)
    child2.send("sudo poweroff")
    time.sleep(0.3)
    child2.send("\x0d")

    try:
        child2.expect(EOF, timeout=60)
    except TIMEOUT:
        pass

    try:
        child2.close(force=False)
    except Exception:
        child2.close(force=True)

    time.sleep(3)

    import subprocess

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    # Assert profile was inherited
    assert profile_inherited, "Should inherit profile 'testprofile' from session on resume"
    assert persistent_inherited, "Should inherit persistent mode from profile on resume"
