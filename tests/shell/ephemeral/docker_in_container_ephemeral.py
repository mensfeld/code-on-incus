"""
Test for coi shell - Docker works inside container.

Tests that:
1. Start shell
2. Run docker commands inside container
3. Verify Docker is functional
"""

import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_docker_in_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that Docker works inside the container.

    Flow:
    1. Start coi shell
    2. Exit claude to bash
    3. Run 'docker --version' to verify Docker is installed
    4. Run 'docker ps' to verify Docker daemon is accessible
    5. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Start session ===

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

    # === Phase 2: Test Docker version ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("docker --version")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(2)
        docker_installed = wait_for_text_in_monitor(monitor, "Docker version", timeout=10)

    # === Phase 3: Test Docker ps (daemon accessible) ===

    with with_live_screen(child) as monitor:
        time.sleep(1)
        child.send("docker ps 2>&1 && echo DOCKER_PS_OK || echo DOCKER_PS_FAILED")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(3)
        # Either docker ps works or we see an error - capture both
        docker_works = wait_for_text_in_monitor(monitor, "DOCKER_PS_OK", timeout=10)
        if not docker_works:
            # Check if it's a permission or daemon issue
            wait_for_text_in_monitor(monitor, "DOCKER_PS_FAILED", timeout=5)

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

    # Assert Docker is available
    assert docker_installed, "Docker should be installed in container"
    assert docker_works, "Docker daemon should be accessible (docker ps should work)"
