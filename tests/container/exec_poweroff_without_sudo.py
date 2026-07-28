"""
Test for coi container exec - poweroff without password.

Tests that:
1. Launch a container
2. Execute sudo poweroff as the code user (no password required)
3. Verify command succeeds (exit code 0)
4. Verify container stops cleanly
"""

import subprocess

from support.helpers import (
    calculate_container_name,
    wait_for_container_started,
    wait_for_container_stopped,
)


def test_exec_poweroff_without_sudo(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that the code user can run sudo poweroff without a password.

    Flow:
    1. Launch a container
    2. Execute sudo poweroff as code user (no password required)
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

    # Wait for the guest to finish booting before issuing poweroff. A poweroff
    # issued while systemd is still coming up can race boot and fail to halt the
    # container (a fixed `time.sleep(3)` left the poweroff-stop poll exhausting
    # its full 90s window on loaded CI runners).
    assert wait_for_container_started(coi_binary, container_name), (
        f"Container {container_name} did not become ready to accept exec"
    )

    # === Phase 2: Execute sudo poweroff (no password required) ===

    # Execute sudo poweroff as the code user (uid 1000) - no password required
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--user",
            "1000",
            "--",
            "sudo",
            "poweroff",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # sudo poweroff should succeed (exit code 0) without requiring a password
    assert result.returncode == 0, (
        f"sudo poweroff should succeed without password. stderr: {result.stderr}"
    )

    # === Phase 3: Wait for container to stop ===

    # Poll until the container stops. Budget 120s: a fixed 30s window flaked on a
    # loaded CI runner (poweroff exited 0 but systemd took >30s to halt) — stock
    # systemd stop budgets run to ~90s (see the #597 shutdown-detection notes),
    # and a heavily-parallel runner can exceed even that, so the wait covers the
    # full graceful-shutdown window with headroom.
    stopped, last = wait_for_container_stopped(coi_binary, container_name, timeout=120)

    # === Phase 4: Verify container stopped ===

    assert stopped, (
        "Container should be stopped after poweroff within 120s. "
        f"Last exit code: {last.returncode}, Output: {last.stdout + last.stderr}"
    )

    # === Phase 5: Cleanup ===

    # Clean up the stopped container
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
