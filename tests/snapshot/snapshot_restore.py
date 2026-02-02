"""
Test for coi snapshot restore - restoring container from snapshot.

Tests that:
1. Restore requires stopped container
2. Restore with --force skips confirmation
3. Restore nonexistent snapshot fails
4. Successful restore message
5. Restore recovers deleted files
"""

import json
import subprocess
import time

from support.helpers import calculate_container_name


def test_snapshot_restore_requires_stopped_container(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that restore fails when container is running.

    Flow:
    1. Launch a container (running)
    2. Create a snapshot
    3. Try to restore while running
    4. Verify error about stopping container
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "restore-test"

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

    # === Phase 3: Try to restore while running ===
    result = subprocess.run(
        [coi_binary, "snapshot", "restore", snapshot_name, "-c", container_name, "-f"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Restore should fail when container is running"
    assert "must be stopped" in result.stderr, "Should mention container must be stopped"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_restore_with_force(coi_binary, cleanup_containers, workspace_dir):
    """
    Test successful restore with --force flag (skip confirmation).

    Flow:
    1. Launch a container
    2. Create a snapshot
    3. Create a file in container
    4. Stop container
    5. Restore from snapshot with --force
    6. Verify restore succeeded
    7. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "force-restore-test"

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

    # === Phase 3: Create a file after snapshot ===
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "touch", "/tmp/after-snapshot.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Creating file should succeed. stderr: {result.stderr}"

    # Verify file exists
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ls", "/tmp/after-snapshot.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "File should exist before restore"

    # === Phase 4: Stop container ===
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    # === Phase 5: Restore from snapshot with --force ===
    result = subprocess.run(
        [coi_binary, "snapshot", "restore", snapshot_name, "-c", container_name, "-f"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Restore should succeed. stderr: {result.stderr}"
    assert "Restored container" in result.stderr, "Should confirm restore"

    # === Phase 6: Start container and verify file is gone ===
    result = subprocess.run(
        [coi_binary, "container", "start", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container start should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # File should not exist after restore (was created after snapshot)
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ls", "/tmp/after-snapshot.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "File should not exist after restore"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_restore_nonexistent_snapshot(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that restoring nonexistent snapshot fails.

    Flow:
    1. Launch a container
    2. Stop container
    3. Try to restore nonexistent snapshot
    4. Verify error message
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

    # === Phase 2: Stop container ===
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    # === Phase 3: Try to restore nonexistent snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "restore", "nonexistent-snapshot", "-c", container_name, "-f"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Restore should fail for nonexistent snapshot"
    assert "not found" in result.stderr, "Should mention snapshot not found"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_restore_nonexistent_container(coi_binary):
    """
    Test that restore fails for nonexistent container.
    """
    result = subprocess.run(
        [coi_binary, "snapshot", "restore", "test-snap", "-c", "nonexistent-container-xyz", "-f"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Should fail for nonexistent container"
    assert "not found" in result.stderr, "Should mention container not found"


def test_snapshot_restore_recovers_deleted_file(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that restoring snapshot recovers files that were deleted after snapshot.

    Flow:
    1. Launch a container
    2. Create a test file
    3. Create a snapshot (file included in snapshot)
    4. Delete the test file
    5. Stop container
    6. Restore from snapshot
    7. Start container
    8. Verify file is restored
    9. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "recover-test"
    test_file = "/root/test-file.txt"
    test_content = "This file was created before snapshot"

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create test file BEFORE snapshot ===
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            f"echo '{test_content}' > {test_file}",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Creating file should succeed. stderr: {result.stderr}"

    # Verify file exists and has correct content
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--capture", "--", "cat", test_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "File should exist before snapshot"
    # Parse JSON output from --capture
    output = json.loads(result.stdout)
    assert output["exit_code"] == 0, "Command should succeed"
    assert test_content in output["stdout"], "File should have correct content"

    # === Phase 3: Create snapshot (with file) ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot create should succeed. stderr: {result.stderr}"

    # === Phase 4: Delete the test file ===
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "rm", test_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Deleting file should succeed. stderr: {result.stderr}"

    # Verify file is gone
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ls", test_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "File should be deleted before restore"

    # === Phase 5: Stop container ===
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    # === Phase 6: Restore from snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "restore", snapshot_name, "-c", container_name, "-f"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Restore should succeed. stderr: {result.stderr}"
    assert "Restored container" in result.stderr, "Should confirm restore"

    # === Phase 7: Start container ===
    result = subprocess.run(
        [coi_binary, "container", "start", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container start should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 8: Verify file is restored with original content ===
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--capture", "--", "cat", test_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "File should be restored after snapshot restore"
    # Parse JSON output
    output = json.loads(result.stdout)
    assert output["exit_code"] == 0, "Command should succeed"
    assert test_content in output["stdout"], "File should have original content after restore"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_restore_missing_name(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that restore requires snapshot name argument.
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

    # === Phase 2: Try to restore without name ===
    result = subprocess.run(
        [coi_binary, "snapshot", "restore", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Restore should fail without snapshot name"
    # Cobra should show usage error
    assert "accepts 1 arg" in result.stderr or "required" in result.stderr.lower(), (
        "Should indicate argument required"
    )

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
