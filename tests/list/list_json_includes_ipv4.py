"""Test coi list --format=json includes IPv4 field"""

import json
import subprocess
import time

from support.helpers import calculate_container_name


def test_list_json_includes_ipv4(coi_binary, cleanup_containers, workspace_dir):
    """Test that coi list --format=json includes ipv4 field for containers."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Phase 1: Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch failed: {result.stderr}"

    time.sleep(3)

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

    # Find our container
    container = None
    for c in data["active_containers"]:
        if c["name"] == container_name:
            container = c
            break

    assert container is not None, f"Container {container_name} not found in output"

    # Verify ipv4 field exists
    assert "ipv4" in container, "Missing ipv4 field"

    # For running container, IPv4 should be a non-empty string
    assert isinstance(container["ipv4"], str), "ipv4 should be a string"
    assert container["ipv4"] != "", "Running container should have an IPv4 address"

    # Verify it looks like an IP address (basic check)
    assert "." in container["ipv4"], (
        f"IPv4 should look like an IP address, got: {container['ipv4']}"
    )

    # Phase 4: Stop container and verify IPv4 becomes empty
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Stop failed: {result.stderr}"

    time.sleep(2)

    # Phase 5: Check JSON again
    result = subprocess.run(
        [coi_binary, "list", "--format=json"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List failed: {result.stderr}"

    data = json.loads(result.stdout)

    # Find our container again
    container = None
    for c in data["active_containers"]:
        if c["name"] == container_name:
            container = c
            break

    assert container is not None, f"Container {container_name} not found in output"

    # Verify ipv4 field exists but is empty for stopped container
    assert "ipv4" in container, "Missing ipv4 field for stopped container"
    assert container["ipv4"] == "", (
        f"Stopped container should have empty IPv4, got: {container['ipv4']}"
    )

    # Phase 6: Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
