"""
Test that `coi list --format=json` includes a `pool` field per container.
"""

import json
import subprocess
import time

from support.helpers import calculate_container_name


def test_list_pool_column_json(coi_binary, cleanup_containers, workspace_dir):
    """JSON output should include a 'pool' field for each container."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch a container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch should succeed. stderr: {result.stderr}"

    time.sleep(2)

    result = subprocess.run(
        [coi_binary, "list", "--format=json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"list should succeed. stderr: {result.stderr}"

    data = json.loads(result.stdout)
    assert "active_containers" in data, "Should have active_containers key"

    # Find our container
    container = None
    for c in data["active_containers"]:
        if c.get("name") == container_name:
            container = c
            break

    assert container is not None, f"Container {container_name} not found in JSON output"
    assert "pool" in container, (
        f"Container entry should include 'pool' field. Got keys: {list(container.keys())}"
    )
    # Pool should be a non-empty string for a running container
    assert isinstance(container["pool"], str), (
        f"pool should be a string. Got: {type(container['pool'])}"
    )
    assert container["pool"] != "", (
        f"pool should be non-empty for a running container. Got: {container}"
    )

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
