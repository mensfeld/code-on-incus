"""
Test for coi persist --all flag.

Tests that:
1. Launch 2 ephemeral containers
2. Create session metadata for both
3. Run coi persist --all --force
4. Verify both containers' metadata updated
5. Verify success message shows "Persisted 2"
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


def test_persist_all_flag(coi_binary, cleanup_containers, workspace_dir):
    """
    Test persist --all flag on multiple containers.

    Flow:
    1. Launch 2 ephemeral containers
    2. Create session metadata for both
    3. Persist all with --all --force
    4. Verify both metadata files updated
    5. Verify success output
    """
    container_name_1 = calculate_container_name(workspace_dir, 1)
    container_name_2 = calculate_container_name(workspace_dir, 2)

    # === Phase 1: Launch 2 containers ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name_1],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container 1 launch should succeed. stderr: {result.stderr}"

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name_2],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container 2 launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 1.5: Create session metadata for both containers ===

    create_session_metadata(container_name_1, workspace_dir, persistent=False)
    create_session_metadata(container_name_2, workspace_dir, persistent=False)

    # === Phase 2: Persist all containers ===

    result = subprocess.run(
        [coi_binary, "persist", "--all", "--force"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Persist --all should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    # Check that persist operation succeeded (may persist more than 2 if other test containers exist)
    assert "Persisted" in combined_output, (
        f"Should show persisted confirmation. Got:\n{combined_output}"
    )
    # Verify both our containers were persisted
    assert container_name_1 in combined_output, (
        f"Should persist {container_name_1}. Got:\n{combined_output}"
    )
    assert container_name_2 in combined_output, (
        f"Should persist {container_name_2}. Got:\n{combined_output}"
    )

    # === Phase 3: Verify both metadata files updated ===

    sessions_dir = Path.home() / ".coi" / "sessions-claude"
    assert sessions_dir.exists(), f"Sessions directory should exist: {sessions_dir}"

    # Helper to find metadata for a container
    def find_metadata(container_name):
        for session_dir in sessions_dir.iterdir():
            if not session_dir.is_dir():
                continue

            metadata_path = session_dir / "metadata.json"
            if not metadata_path.exists():
                continue

            try:
                with open(metadata_path) as f:
                    metadata = json.load(f)

                if metadata.get("container_name") == container_name:
                    return metadata
            except (json.JSONDecodeError, KeyError):
                continue

        return None

    # Check container 1 metadata
    metadata_1 = find_metadata(container_name_1)
    assert metadata_1 is not None, f"Should find metadata for container {container_name_1}"
    assert metadata_1["persistent"] is True, "Container 1 should be persistent"

    # Check container 2 metadata
    metadata_2 = find_metadata(container_name_2)
    assert metadata_2 is not None, f"Should find metadata for container {container_name_2}"
    assert metadata_2["persistent"] is True, "Container 2 should be persistent"
