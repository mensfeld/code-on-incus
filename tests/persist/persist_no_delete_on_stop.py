"""
Test for coi persist - verify container not deleted on stop.

Tests that:
1. Launch ephemeral container
2. Create session metadata
3. Persist it
4. Stop container (not delete)
5. Verify container still exists after stop
"""

import json
import subprocess
import time
import uuid
from datetime import datetime
from pathlib import Path

from support.helpers import calculate_container_name


def create_session_metadata(container_name, workspace_dir, persistent=False):
    """Create a session metadata file for a container."""
    sessions_dir = Path.home() / ".coi" / "sessions-claude"
    sessions_dir.mkdir(parents=True, exist_ok=True)

    session_id = str(uuid.uuid4())
    session_dir = sessions_dir / session_id
    session_dir.mkdir(parents=True, exist_ok=True)

    metadata = {
        "session_id": session_id,
        "container_name": container_name,
        "persistent": persistent,
        "workspace": str(workspace_dir),
        "saved_at": datetime.now().isoformat() + "Z",
    }

    metadata_path = session_dir / "metadata.json"
    with open(metadata_path, "w") as f:
        json.dump(metadata, f, indent=2)

    return metadata_path


def test_persist_no_delete_on_stop(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that persisted containers are not deleted when stopped.

    Flow:
    1. Launch ephemeral container
    2. Create session metadata
    3. Persist it
    4. Stop the container
    5. Verify container still exists (not deleted)
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch ephemeral container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # Verify container is running
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Container should be running. stderr: {result.stderr}"

    # === Phase 1.5: Create session metadata ===

    create_session_metadata(container_name, workspace_dir, persistent=False)

    # === Phase 2: Persist the container ===

    result = subprocess.run(
        [coi_binary, "persist", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Persist should succeed. stderr: {result.stderr}"

    # === Phase 3: Stop the container ===

    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    time.sleep(5)  # Wait for stop to complete

    # === Phase 4: Verify container still exists ===

    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, (
        f"Container should still exist after stop (persistent mode). stdout: {result.stdout}"
    )

    # Verify it's stopped (not running)
    result = subprocess.run(
        [coi_binary, "container", "running", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Container should not be running after stop"
