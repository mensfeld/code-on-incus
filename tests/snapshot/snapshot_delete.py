"""
Test for coi snapshot delete - deleting container snapshots.

Tests that:
1. Can delete a single snapshot
2. Delete all snapshots with --all flag
3. --force flag skips confirmation
4. Cannot delete nonexistent snapshot
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_snapshot_delete_single(coi_binary, cleanup_containers, workspace_dir):
    """
    Test deleting a single snapshot.

    Flow:
    1. Launch a container
    2. Create a snapshot
    3. Delete the snapshot
    4. Verify snapshot is gone
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "delete-test"

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot create should succeed. stderr: {result.stderr}"

    # Verify snapshot exists
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert snapshot_name in result.stdout, "Snapshot should exist before deletion"

    # === Phase 3: Delete snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "delete", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot delete should succeed. stderr: {result.stderr}"
    assert "Deleted snapshot" in result.stderr, "Should confirm deletion"

    # === Phase 4: Verify snapshot is gone ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert snapshot_name not in result.stdout, "Snapshot should be deleted"
    assert "(none)" in result.stdout or "Total: 0 snapshots" in result.stdout, (
        "Should show empty list"
    )

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_delete_all_with_force(coi_binary, cleanup_containers, workspace_dir):
    """
    Test deleting all snapshots with --all and --force flags.

    Flow:
    1. Launch a container
    2. Create multiple snapshots
    3. Delete all with --all --force
    4. Verify all snapshots are gone
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

    # === Phase 2: Create multiple snapshots ===
    for name in ["snap1", "snap2", "snap3"]:
        result = subprocess.run(
            [coi_binary, "snapshot", "create", name, "-c", container_name],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode == 0, f"Snapshot {name} should succeed. stderr: {result.stderr}"

    # Verify all exist
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert "Total: 3 snapshots" in result.stdout, "Should have 3 snapshots before deletion"

    # === Phase 3: Delete all with --all --force ===
    result = subprocess.run(
        [coi_binary, "snapshot", "delete", "--all", "-f", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Delete all should succeed. stderr: {result.stderr}"
    assert "Deleted snapshot 'snap1'" in result.stderr, "Should confirm snap1 deletion"
    assert "Deleted snapshot 'snap2'" in result.stderr, "Should confirm snap2 deletion"
    assert "Deleted snapshot 'snap3'" in result.stderr, "Should confirm snap3 deletion"

    # === Phase 4: Verify all snapshots are gone ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert "(none)" in result.stdout or "Total: 0 snapshots" in result.stdout, (
        "Should show empty list"
    )

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_delete_all_empty(coi_binary, cleanup_containers, workspace_dir):
    """
    Test delete --all when there are no snapshots.

    Flow:
    1. Launch a container
    2. Try to delete all (without creating any)
    3. Verify appropriate message
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

    # === Phase 2: Delete all (no snapshots exist) ===
    result = subprocess.run(
        [coi_binary, "snapshot", "delete", "--all", "-f", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, (
        f"Delete all should succeed even with no snapshots. stderr: {result.stderr}"
    )
    assert "No snapshots to delete" in result.stderr, "Should indicate no snapshots"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_delete_nonexistent_snapshot(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that deleting nonexistent snapshot fails.

    Flow:
    1. Launch a container
    2. Try to delete nonexistent snapshot
    3. Verify error message
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

    # === Phase 2: Try to delete nonexistent snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "delete", "nonexistent-snapshot", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Delete should fail for nonexistent snapshot"
    assert "not found" in result.stderr, "Should mention snapshot not found"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_delete_nonexistent_container(coi_binary):
    """
    Test that delete fails for nonexistent container.
    """
    result = subprocess.run(
        [coi_binary, "snapshot", "delete", "test-snap", "-c", "nonexistent-container-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Should fail for nonexistent container"
    assert "not found" in result.stderr, "Should mention container not found"


def test_snapshot_delete_missing_name_without_all(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that delete requires snapshot name or --all flag.
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

    # === Phase 2: Try to delete without name or --all ===
    result = subprocess.run(
        [coi_binary, "snapshot", "delete", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Delete should fail without snapshot name or --all"
    assert "snapshot name required" in result.stderr or "--all" in result.stderr, (
        "Should indicate name or --all required"
    )

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_delete_selective(coi_binary, cleanup_containers, workspace_dir):
    """
    Test deleting one snapshot while keeping others.

    Flow:
    1. Launch a container
    2. Create multiple snapshots
    3. Delete one specific snapshot
    4. Verify only that snapshot is deleted
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

    # === Phase 2: Create multiple snapshots ===
    for name in ["keep1", "delete-me", "keep2"]:
        result = subprocess.run(
            [coi_binary, "snapshot", "create", name, "-c", container_name],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode == 0, f"Snapshot {name} should succeed. stderr: {result.stderr}"

    # === Phase 3: Delete one specific snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "delete", "delete-me", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot delete should succeed. stderr: {result.stderr}"

    # === Phase 4: Verify selective deletion ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert "keep1" in result.stdout, "keep1 should still exist"
    assert "keep2" in result.stdout, "keep2 should still exist"
    assert "delete-me" not in result.stdout, "delete-me should be deleted"
    assert "Total: 2 snapshots" in result.stdout, "Should have 2 remaining snapshots"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
