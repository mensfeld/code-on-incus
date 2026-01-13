"""Test coi list --format=json with active containers"""

import json
import subprocess

from support.helpers import calculate_container_name


def test_list_format_json_active(coi_binary, cleanup_containers, workspace_dir):
    """Test that coi list --format=json outputs valid JSON with active containers."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Phase 1: Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch failed: {result.stderr}"

    # Phase 2: Run list with JSON format
    result = subprocess.run(
        [coi_binary, "list", "--format=json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List failed: {result.stderr}"

    # Phase 3: Parse and verify JSON
    data = json.loads(result.stdout)

    # Verify structure
    assert "active_containers" in data, "Missing 'active_containers' key"
    assert isinstance(data["active_containers"], list), "active_containers should be a list"
    assert len(data["active_containers"]) > 0, "Should have at least one container"

    # Find our container
    container = None
    for c in data["active_containers"]:
        if c["name"] == container_name:
            container = c
            break

    assert container is not None, f"Container {container_name} not found in output"

    # Verify container fields
    assert container["status"] == "Running", "Container should be running"
    assert "created_at" in container, "Missing created_at field"
    assert "image" in container, "Missing image field"
    assert "persistent" in container, "Missing persistent field"
    assert isinstance(container["persistent"], bool), "persistent should be boolean"

    # Phase 4: Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
