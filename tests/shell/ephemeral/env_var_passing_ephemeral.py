"""
Test for coi shell - passing environment variables to container via config file.

Tests that:
1. Create .coi/config.toml with [defaults.environment] to set env var
2. Start shell session
3. Verify environment variable is set inside container
"""

import subprocess
import time
from pathlib import Path

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_env_var_passing(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that environment variables from config file are passed to container.

    Flow:
    1. Create .coi/config.toml with [defaults.environment] COI_TEST_CUSTOM_VAR = "custom_value_98765"
    2. Start coi shell
    3. Exit claude to bash
    4. Echo $COI_TEST_CUSTOM_VAR and verify it's set
    5. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    test_var_name = "COI_TEST_CUSTOM_VAR"
    test_var_value = "custom_value_98765"

    # === Phase 1: Create config file with environment variable ===

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults.environment]
{test_var_name} = "{test_var_value}"
"""
    )

    # === Phase 2: Start session ===

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

    # === Phase 3: Check environment variable ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send(f"echo VAR_CHECK_${{{test_var_name}}}_END")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        var_set = wait_for_text_in_monitor(monitor, f"VAR_CHECK_{test_var_value}_END", timeout=10)

    # === Phase 4: Cleanup ===

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
