"""
Test for coi snapshot info - showing detailed snapshot information.

Tests that:
1. Can show snapshot info in text format
2. Can show snapshot info in JSON format
3. Nonexistent snapshot returns error
4. Info shows correct metadata (name, created time, stateful status)
"""

import json
import subprocess
import time

from support.helpers import calculate_container_name


def test_snapshot_info_text_format(coi_binary, cleanup_containers, workspace_dir):
    """
    Test showing snapshot info in text format.

    Flow:
    1. Launch a container
    2. Create a snapshot
    3. Show info in text format
    4. Verify output contains key fields
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "info-test"

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

    # === Phase 3: Show info in text format ===
    result = subprocess.run(
        [coi_binary, "snapshot", "info", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot info should succeed. stderr: {result.stderr}"

    # Verify text format output
    assert f"Snapshot: {snapshot_name}" in result.stdout, "Should show snapshot name"
    assert f"Container: {container_name}" in result.stdout, "Should show container name"
    assert "Created:" in result.stdout, "Should show creation time"
    assert "Stateful:" in result.stdout, "Should show stateful status"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_info_json_format(coi_binary, cleanup_containers, workspace_dir):
    """
    Test showing snapshot info in JSON format.

    Flow:
    1. Launch a container
    2. Create a snapshot
    3. Show info in JSON format
    4. Verify JSON structure
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "json-info-test"

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

    # === Phase 3: Show info in JSON format ===
    result = subprocess.run(
        [coi_binary, "snapshot", "info", snapshot_name, "-c", container_name, "--format", "json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Snapshot info should succeed. stderr: {result.stderr}"

    # Parse and verify JSON
    data = json.loads(result.stdout)
    assert "container" in data, "JSON should have container field"
    assert "snapshot" in data, "JSON should have snapshot field"
    assert data["container"] == container_name, "Container name should match"
    assert data["snapshot"]["name"] == snapshot_name, "Snapshot name should match"
    assert "created_at" in data["snapshot"], "Snapshot should have created_at"
    assert "stateful" in data["snapshot"], "Snapshot should have stateful field"
    assert data["snapshot"]["stateful"] is False, "Snapshot should not be stateful (default)"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_info_nonexistent_snapshot(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that info fails for nonexistent snapshot.

    Flow:
    1. Launch a container
    2. Try to get info for nonexistent snapshot
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

    # === Phase 2: Try to get info for nonexistent snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "info", "nonexistent-snapshot", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Info should fail for nonexistent snapshot"
    assert "not found" in result.stderr, "Should mention snapshot not found"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_snapshot_info_nonexistent_container(coi_binary):
    """
    Test that info fails for nonexistent container.
    """
    result = subprocess.run(
        [coi_binary, "snapshot", "info", "test-snap", "-c", "nonexistent-container-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Should fail for nonexistent container"
    assert "not found" in result.stderr, "Should mention container not found"


def test_snapshot_info_invalid_format(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that invalid format value is rejected.
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "format-test"

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

    # === Phase 3: Try invalid format ===
    result = subprocess.run(
        [
            coi_binary,
            "snapshot",
            "info",
            snapshot_name,
            "-c",
            container_name,
            "--format",
            "invalid",
        ],
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


def test_snapshot_info_missing_name(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that info requires snapshot name argument.
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

    # === Phase 2: Try to get info without name ===
    result = subprocess.run(
        [coi_binary, "snapshot", "info", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Info should fail without snapshot name"
    assert "accepts 1 arg" in result.stderr or "required" in result.stderr.lower(), (
        "Should indicate argument required"
    )

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
