"""
Test for coi persist - basic persist operation.

Tests that:
1. Launch an ephemeral container
2. Create session metadata with persistent: false
3. Run coi persist <container-name>
4. Verify metadata updated to persistent: true
5. Verify container still exists
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


def test_persist_basic(coi_binary, cleanup_containers, workspace_dir):
    """
    Test basic persist operation on a single container.

    Flow:
    1. Launch ephemeral container
    2. Create session metadata with persistent: false
    3. Persist the container
    4. Verify metadata shows persistent: true
    5. Verify container still exists
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

    # === Phase 2: Create session metadata with persistent: false ===

    metadata_path = create_session_metadata(container_name, workspace_dir, persistent=False)

    # Verify persistent is false
    with open(metadata_path) as f:
        metadata = json.load(f)

    assert metadata["persistent"] is False, "Container should initially be ephemeral"

    # === Phase 3: Persist the container ===

    result = subprocess.run(
        [coi_binary, "persist", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Persist should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Persisted" in combined_output or "persisted" in combined_output.lower(), (
        f"Should show persisted confirmation. Got:\n{combined_output}"
    )

    # === Phase 4: Verify metadata updated to persistent: true ===

    with open(metadata_path) as f:
        metadata = json.load(f)

    assert metadata["persistent"] is True, "Container should now be persistent"

    # === Phase 5: Verify container still exists ===

    result = subprocess.run(
        [coi_binary, "container", "exists", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Container should still exist. stdout: {result.stdout}"
