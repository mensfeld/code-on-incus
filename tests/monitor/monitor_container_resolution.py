"""
Test for coi monitor container resolution strategy.

Tests that:
1. Auto-resolves container from workspace when only one exists
2. Fails with error when no containers exist for workspace
3. Uses explicit container argument when provided
4. Uses COI_CONTAINER env var when set
5. Uses --workspace flag for workspace path
"""

import json
import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_monitor_auto_resolve_single_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test auto-resolving container when exactly one exists for workspace.

    Flow:
    1. Launch a container for the workspace
    2. Run coi monitor --json without container arg from workspace dir
    3. Verify JSON output contains container name
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Run monitor without container arg ===
    result = subprocess.run(
        [coi_binary, "monitor", "--json", "-w", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"Monitor should auto-resolve container. stderr: {result.stderr}"
    assert f"Monitoring container: {container_name}" in result.stderr, (
        "Should print resolved container name to stderr"
    )

    # Verify JSON output is valid and contains container info
    data = json.loads(result.stdout)
    assert "container" in data, "JSON output should contain container field"
    assert data["container"] == container_name, "JSON should reference correct container"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_monitor_no_container_for_workspace(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that monitor fails when no container exists for workspace.

    Flow:
    1. Try to run monitor without launching container first
    2. Verify error message about no containers
    """
    result = subprocess.run(
        [coi_binary, "monitor", "--json", "-w", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert result.returncode != 0, "Should fail when no containers exist for workspace"
    assert "no coi containers found" in result.stderr.lower(), (
        f"Should mention no containers found. stderr: {result.stderr}"
    )


def test_monitor_explicit_container_arg(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that explicit container argument works.

    Flow:
    1. Launch a container
    2. Run coi monitor <name> --json
    3. Verify success
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Run monitor with explicit container arg ===
    result = subprocess.run(
        [coi_binary, "monitor", container_name, "--json"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, (
        f"Monitor should succeed with explicit container arg. stderr: {result.stderr}"
    )
    assert f"Monitoring container: {container_name}" in result.stderr, (
        "Should print resolved container name to stderr"
    )

    data = json.loads(result.stdout)
    assert data["container"] == container_name, "JSON should reference correct container"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_monitor_env_var_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that COI_CONTAINER environment variable is used.

    Flow:
    1. Launch a container
    2. Set COI_CONTAINER env var
    3. Run coi monitor --json without container arg
    4. Verify success
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Run monitor with COI_CONTAINER env var ===
    env = os.environ.copy()
    env["COI_CONTAINER"] = container_name

    result = subprocess.run(
        [coi_binary, "monitor", "--json"],
        capture_output=True,
        text=True,
        timeout=60,
        env=env,
    )
    assert result.returncode == 0, (
        f"Monitor should use COI_CONTAINER env var. stderr: {result.stderr}"
    )
    assert f"Monitoring container: {container_name}" in result.stderr, (
        "Should print resolved container name to stderr"
    )

    data = json.loads(result.stdout)
    assert data["container"] == container_name, "JSON should reference correct container"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_monitor_workspace_flag(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that --workspace flag works for container resolution from a different cwd.

    Flow:
    1. Launch a container for the workspace
    2. Run coi monitor -w <workspace_dir> --json from /tmp
    3. Verify success
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Run monitor with -w flag from a different directory ===
    result = subprocess.run(
        [coi_binary, "monitor", "--json", "-w", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd="/tmp",
    )
    assert result.returncode == 0, (
        f"Monitor should resolve container via -w flag. stderr: {result.stderr}"
    )
    assert f"Monitoring container: {container_name}" in result.stderr, (
        "Should print resolved container name to stderr"
    )

    data = json.loads(result.stdout)
    assert data["container"] == container_name, "JSON should reference correct container"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
