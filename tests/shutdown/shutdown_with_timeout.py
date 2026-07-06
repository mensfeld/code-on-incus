"""
Test for the [container] shutdown_timeout config option (the former
--timeout flag was removed — config-shaped settings live in config/profiles).

Tests that:
1. Launch a container
2. Shutdown with a custom shutdown_timeout from config
3. Verify the timeout value is shown in output
4. The removed --timeout flag fails with the migration hint
"""

import subprocess
import time

from support.helpers import calculate_container_name, write_trusted_coi_config


def test_shutdown_with_timeout(coi_binary, cleanup_containers, workspace_dir):
    """
    Test shutting down with custom timeout.

    Flow:
    1. Launch a container
    2. Run coi shutdown <container> with [container] shutdown_timeout = 5
    3. Verify timeout is shown in output
    """
    env = write_trusted_coi_config("[container]\nshutdown_timeout = 5\n")
    slot = 1
    container_name = calculate_container_name(workspace_dir, slot)

    # Launch a container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # Shutdown with the config-driven timeout
    result = subprocess.run(
        [coi_binary, "shutdown", container_name],
        capture_output=True,
        text=True,
        timeout=120,
        env=env,
    )

    assert result.returncode == 0, f"Shutdown should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    # Should show the config-driven timeout value (5s)
    assert "timeout: 5s" in combined_output, (
        f"Should show the configured timeout in output. Got:\n{combined_output}"
    )
    assert "shutdown" in combined_output.lower(), (
        f"Should show shutdown message. Got:\n{combined_output}"
    )


def test_shutdown_timeout_flag_removed(coi_binary):
    """The removed --timeout flag fails with the config migration hint."""
    result = subprocess.run(
        [coi_binary, "shutdown", "--timeout=5", "whatever"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "removed --timeout flag must fail"
    assert "flag was removed" in result.stderr, f"want migration hint, got:\n{result.stderr}"
    assert "[container] shutdown_timeout" in result.stderr, (
        f"hint must point at the config key, got:\n{result.stderr}"
    )
