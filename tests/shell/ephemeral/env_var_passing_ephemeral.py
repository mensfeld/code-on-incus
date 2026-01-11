"""
Test for coi shell --env - passing environment variables to container.

Tests that:
1. Start shell with --env KEY=VALUE
2. Verify environment variable is set inside container
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


def test_env_var_passing(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that --env flag passes environment variables to container.

    Flow:
    1. Start coi shell --env TEST_VAR=hello123
    2. Exit claude to bash
    3. Echo $TEST_VAR and verify it's set
    4. Cleanup
    """
    env = {"COI_USE_TEST_CLAUDE": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    test_var_name = "COI_TEST_CUSTOM_VAR"
    test_var_value = "custom_value_98765"

    # === Phase 1: Start session with custom env var ===

    child = spawn_coi(
        coi_binary,
        ["shell", f"--env={test_var_name}={test_var_value}"],
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

    # === Phase 2: Check environment variable ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send(f"echo VAR_CHECK_${{{test_var_name}}}_END")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        var_set = wait_for_text_in_monitor(monitor, f"VAR_CHECK_{test_var_value}_END", timeout=10)

    # === Phase 3: Cleanup ===

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

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    # Assert env var was passed
    assert var_set, f"Environment variable {test_var_name} should be set to {test_var_value}"
