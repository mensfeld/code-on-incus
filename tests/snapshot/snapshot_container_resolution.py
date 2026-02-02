"""
Test for coi snapshot container resolution strategy.

Tests that:
1. Auto-resolves container from workspace when only one exists
2. Fails with error when no containers exist for workspace
3. Fails with error when multiple containers exist
4. Uses --container flag when provided
5. Uses COI_CONTAINER env var when set
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_snapshot_auto_resolve_single_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test auto-resolving container when exactly one exists for workspace.

    Flow:
    1. Launch a container for the workspace
    2. Create a snapshot without specifying --container
    3. Verify snapshot is created for correct container
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "auto-resolve-test"

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create snapshot without --container flag ===
    # Change to workspace directory so container resolution works
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-w", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, (
        f"Snapshot create should auto-resolve container. stderr: {result.stderr}"
    )
    assert f"Created snapshot '{snapshot_name}'" in result.stderr, (
        "Should confirm snapshot creation"
    )

    # === Phase 3: Verify snapshot exists for correct container ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert snapshot_name in result.stdout, "Snapshot should exist for resolved container"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_no_container_for_workspace(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that snapshot fails when no container exists for workspace.

    Flow:
    1. Try to create snapshot without launching container first
    2. Verify error message about no containers
    """
    result = subprocess.run(
        [coi_binary, "snapshot", "create", "test-snap", "-w", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert result.returncode != 0, "Should fail when no containers exist for workspace"
    assert (
        "no COI containers found" in result.stderr.lower() or "not found" in result.stderr.lower()
    ), "Should mention no containers found"


def test_snapshot_multiple_containers_requires_flag(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that snapshot fails when multiple containers exist for workspace.

    Flow:
    1. Launch two containers for the workspace (slots 1 and 2)
    2. Try to create snapshot without --container
    3. Verify error about multiple containers
    4. Create snapshot with explicit --container
    5. Cleanup
    """
    container1 = calculate_container_name(workspace_dir, 1)
    container2 = calculate_container_name(workspace_dir, 2)
    snapshot_name = "multi-container-test"

    # === Phase 1: Launch two containers ===
    for container in [container1, container2]:
        result = subprocess.run(
            [coi_binary, "container", "launch", "coi", container],
            capture_output=True,
            text=True,
            timeout=120,
        )
        assert result.returncode == 0, (
            f"Container {container} launch should succeed. stderr: {result.stderr}"
        )
        time.sleep(3)

    # === Phase 2: Try to create snapshot without --container ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-w", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert result.returncode != 0, "Should fail when multiple containers exist"
    assert "multiple" in result.stderr.lower() or "use --container" in result.stderr.lower(), (
        "Should mention multiple containers and suggest --container flag"
    )

    # === Phase 3: Create snapshot with explicit --container ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-c", container1, "-w", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, (
        f"Snapshot should succeed with explicit container. stderr: {result.stderr}"
    )

    # Verify snapshot was created for correct container
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container1],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert snapshot_name in result.stdout, "Snapshot should exist for specified container"

    # Verify snapshot doesn't exist for other container
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container2],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert snapshot_name not in result.stdout, "Snapshot should not exist for other container"

    # === Cleanup ===
    for container in [container1, container2]:
        subprocess.run(
            [coi_binary, "container", "delete", container, "--force"],
            capture_output=True,
            timeout=30,
        )


def test_snapshot_explicit_container_flag(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that --container flag takes precedence.

    Flow:
    1. Launch a container
    2. Create snapshot with explicit --container flag
    3. Verify snapshot is created for specified container
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "explicit-container-test"

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create snapshot with explicit --container ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "--container", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot create should succeed. stderr: {result.stderr}"

    # === Phase 3: Verify snapshot exists ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert snapshot_name in result.stdout, "Snapshot should exist for specified container"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_env_var_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that COI_CONTAINER environment variable is used.

    Flow:
    1. Launch a container
    2. Set COI_CONTAINER env var
    3. Create snapshot without --container flag
    4. Verify snapshot is created for env var container
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "env-var-test"

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create snapshot with COI_CONTAINER env var ===
    env = os.environ.copy()
    env["COI_CONTAINER"] = container_name

    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name],
        capture_output=True,
        text=True,
        timeout=60,
        env=env,
    )
    assert result.returncode == 0, (
        f"Snapshot create should use COI_CONTAINER env var. stderr: {result.stderr}"
    )

    # === Phase 3: Verify snapshot exists ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert snapshot_name in result.stdout, "Snapshot should exist for env var container"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_container_flag_overrides_env(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that --container flag takes precedence over COI_CONTAINER env var.

    Flow:
    1. Launch two containers
    2. Set COI_CONTAINER to one container
    3. Create snapshot with --container pointing to different container
    4. Verify snapshot is created for container from flag, not env var
    5. Cleanup
    """
    container1 = calculate_container_name(workspace_dir, 1)
    container2 = calculate_container_name(workspace_dir, 2)
    snapshot_name = "override-test"

    # === Phase 1: Launch two containers ===
    for container in [container1, container2]:
        result = subprocess.run(
            [coi_binary, "container", "launch", "coi", container],
            capture_output=True,
            text=True,
            timeout=120,
        )
        assert result.returncode == 0, (
            f"Container {container} launch should succeed. stderr: {result.stderr}"
        )
        time.sleep(3)

    # === Phase 2: Create snapshot with flag overriding env ===
    env = os.environ.copy()
    env["COI_CONTAINER"] = container1  # Env points to container1

    result = subprocess.run(
        [
            coi_binary,
            "snapshot",
            "create",
            snapshot_name,
            "-c",
            container2,
        ],  # Flag points to container2
        capture_output=True,
        text=True,
        timeout=60,
        env=env,
    )
    assert result.returncode == 0, f"Snapshot create should succeed. stderr: {result.stderr}"

    # === Phase 3: Verify snapshot is on container2 (from flag), not container1 (from env) ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container2],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert snapshot_name in result.stdout, "Snapshot should exist for container from flag"

    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container1],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert snapshot_name not in result.stdout, (
        "Snapshot should not exist for container from env var"
    )

    # === Cleanup ===
    for container in [container1, container2]:
        subprocess.run(
            [coi_binary, "container", "delete", container, "--force"],
            capture_output=True,
            timeout=30,
        )
