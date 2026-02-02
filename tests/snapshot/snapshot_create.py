"""
Test for coi snapshot create - creating container snapshots.

Tests that:
1. Can create a snapshot with auto-generated name
2. Can create a snapshot with explicit name
3. Cannot create duplicate snapshot names
4. Can create stateful snapshot (with --stateful flag)
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_snapshot_create_auto_name(coi_binary, cleanup_containers, workspace_dir):
    """
    Test creating a snapshot with auto-generated name.

    Flow:
    1. Launch a container
    2. Create a snapshot without specifying name
    3. Verify snapshot was created with auto-generated name
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

    # === Phase 2: Create snapshot with auto-generated name ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot create should succeed. stderr: {result.stderr}"
    assert "Created snapshot" in result.stderr, "Should confirm snapshot creation"
    assert "snap-" in result.stderr, "Auto-generated name should start with snap-"

    # === Phase 3: Verify snapshot exists ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert "snap-" in result.stdout, "Auto-named snapshot should appear in list"
    assert "Total: 1 snapshot" in result.stdout, "Should show 1 snapshot"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_create_explicit_name(coi_binary, cleanup_containers, workspace_dir):
    """
    Test creating a snapshot with explicit name.

    Flow:
    1. Launch a container
    2. Create a snapshot with specific name
    3. Verify snapshot was created with correct name
    4. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "test-checkpoint"

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create snapshot with explicit name ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot create should succeed. stderr: {result.stderr}"
    assert f"Created snapshot '{snapshot_name}'" in result.stderr, (
        "Should confirm snapshot creation with name"
    )

    # === Phase 3: Verify snapshot exists ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert snapshot_name in result.stdout, "Named snapshot should appear in list"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_create_duplicate_name_fails(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that creating a snapshot with duplicate name fails.

    Flow:
    1. Launch a container
    2. Create a snapshot with specific name
    3. Try to create another snapshot with same name
    4. Verify error message
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "duplicate-test"

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create first snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"First snapshot should succeed. stderr: {result.stderr}"

    # === Phase 3: Try to create duplicate ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode != 0, "Duplicate snapshot should fail"
    assert "already exists" in result.stderr, "Should mention snapshot already exists"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_create_multiple(coi_binary, cleanup_containers, workspace_dir):
    """
    Test creating multiple snapshots.

    Flow:
    1. Launch a container
    2. Create multiple snapshots
    3. Verify all snapshots appear in list
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

    # === Phase 2: Create multiple snapshots ===
    for name in ["snap1", "snap2", "snap3"]:
        result = subprocess.run(
            [coi_binary, "snapshot", "create", name, "-c", container_name],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode == 0, f"Snapshot {name} should succeed. stderr: {result.stderr}"

    # === Phase 3: Verify all snapshots exist ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert "snap1" in result.stdout, "snap1 should appear in list"
    assert "snap2" in result.stdout, "snap2 should appear in list"
    assert "snap3" in result.stdout, "snap3 should appear in list"
    assert "Total: 3 snapshots" in result.stdout, "Should show 3 snapshots"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_create_nonexistent_container(coi_binary):
    """
    Test that snapshot create fails for nonexistent container.
    """
    result = subprocess.run(
        [coi_binary, "snapshot", "create", "test-snap", "-c", "nonexistent-container-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Should fail for nonexistent container"
    assert "not found" in result.stderr, "Should mention container not found"
