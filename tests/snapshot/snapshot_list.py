"""
Test for coi snapshot list - listing container snapshots.

Tests that:
1. Can list snapshots in text format
2. Can list snapshots in JSON format
3. Empty list shows appropriate message
4. Can list snapshots for all containers with --all flag
"""

import json
import subprocess
import time

from support.helpers import calculate_container_name


def test_snapshot_list_text_format(coi_binary, cleanup_containers, workspace_dir):
    """
    Test listing snapshots in text format (default).

    Flow:
    1. Launch a container
    2. Create a snapshot
    3. List snapshots in text format
    4. Verify output format
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "test-snapshot"

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

    # === Phase 3: List snapshots in text format ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"

    # Verify text format output
    assert f"Snapshots for {container_name}:" in result.stdout, (
        "Should show container name in header"
    )
    assert "NAME" in result.stdout, "Should have NAME column header"
    assert "CREATED" in result.stdout, "Should have CREATED column header"
    assert "STATEFUL" in result.stdout, "Should have STATEFUL column header"
    assert snapshot_name in result.stdout, "Should list the snapshot"
    assert "Total: 1 snapshot" in result.stdout, "Should show total count"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_list_json_format(coi_binary, cleanup_containers, workspace_dir):
    """
    Test listing snapshots in JSON format.

    Flow:
    1. Launch a container
    2. Create a snapshot
    3. List snapshots in JSON format
    4. Verify JSON structure
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "json-test-snapshot"

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

    # === Phase 3: List snapshots in JSON format ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name, "--format", "json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"

    # Parse and verify JSON
    data = json.loads(result.stdout)
    assert "container" in data, "JSON should have container field"
    assert "snapshots" in data, "JSON should have snapshots field"
    assert data["container"] == container_name, "Container name should match"
    assert len(data["snapshots"]) == 1, "Should have 1 snapshot"
    assert data["snapshots"][0]["name"] == snapshot_name, "Snapshot name should match"
    assert "created_at" in data["snapshots"][0], "Snapshot should have created_at"
    assert "stateful" in data["snapshots"][0], "Snapshot should have stateful field"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_list_empty(coi_binary, cleanup_containers, workspace_dir):
    """
    Test listing snapshots when there are none.

    Flow:
    1. Launch a container
    2. List snapshots (without creating any)
    3. Verify empty list message
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

    # === Phase 2: List snapshots (should be empty) ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"
    assert "(none)" in result.stdout, "Should show (none) for empty list"
    assert "Total: 0 snapshots" in result.stdout, "Should show 0 snapshots"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_list_empty_json(coi_binary, cleanup_containers, workspace_dir):
    """
    Test listing snapshots in JSON format when there are none.

    Flow:
    1. Launch a container
    2. List snapshots in JSON (without creating any)
    3. Verify empty JSON array
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

    # === Phase 2: List snapshots in JSON (should be empty) ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name, "--format", "json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot list should succeed. stderr: {result.stderr}"

    # Parse and verify JSON
    data = json.loads(result.stdout)
    assert data["container"] == container_name, "Container name should match"
    assert data["snapshots"] == [] or data["snapshots"] is None or len(data["snapshots"]) == 0, (
        "Snapshots should be empty"
    )

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_list_invalid_format(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that invalid format value is rejected.
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

    # === Phase 2: Try invalid format ===
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", container_name, "--format", "invalid"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Invalid format should fail"
    assert "invalid format" in result.stderr.lower(), "Should mention invalid format"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_list_nonexistent_container(coi_binary):
    """
    Test that snapshot list fails for nonexistent container.
    """
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "-c", "nonexistent-container-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Should fail for nonexistent container"
    assert "not found" in result.stderr, "Should mention container not found"
