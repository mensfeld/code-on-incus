"""
Test for coi container exec - close command as poweroff alias.

Tests that:
1. Launch a container
2. Execute 'close' as the code user (should behave like poweroff)
3. Verify command succeeds (exit code 0)
4. Verify container stops cleanly
"""

import subprocess

from support.helpers import (
    calculate_container_name,
    wait_for_container_started,
    wait_for_container_stopped,
)


def test_exec_close_alias_poweroff(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that the 'close' command works as an alias for poweroff.

    The 'close' command is a safe alternative to 'poweroff' that doesn't
    exist on the host machine, preventing accidental host shutdowns.

    Flow:
    1. Launch a container
    2. Execute 'close' as code user (should trigger poweroff)
    3. Verify command succeeds
    4. Verify container stops
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    # Wait for the guest to finish booting before issuing the shutdown. Issuing
    # `close` while systemd is still coming up can race boot and leave the
    # container running (a fixed `time.sleep(3)` flaked on loaded CI runners).
    assert wait_for_container_started(coi_binary, container_name), (
        f"Container {container_name} did not become ready to accept exec"
    )

    # === Phase 2: Execute 'close' (poweroff alias, no password required) ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--user",
            "1000",
            "--",
            "close",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # close should succeed (exit code 0) just like poweroff
    assert result.returncode == 0, (
        f"close should succeed as poweroff alias. stderr: {result.stderr}"
    )

    # === Phase 3: Wait for container to stop ===

    # Poll (don't fixed-sleep): a graceful systemd shutdown can take far longer
    # than a few seconds on a loaded CI runner. Budget 120s to cover the full
    # stop window (stock systemd stop budgets run to ~90s).
    stopped, last = wait_for_container_stopped(coi_binary, container_name, timeout=120)

    # === Phase 4: Verify container stopped ===

    assert stopped, (
        "Container should be stopped after close within 120s. "
        f"Last exit code: {last.returncode}, Output: {last.stdout + last.stderr}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
