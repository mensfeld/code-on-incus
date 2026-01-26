"""
Test for coi list - does not show IPv4 for stopped containers.

Tests that:
1. Launch a container
2. Stop it
3. Run coi list
4. Verify it does NOT show IPv4 field (since container is stopped)
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_list_shows_ipv4_stopped(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list does not show IPv4 for stopped containers.

    Flow:
    1. Launch a container
    2. Stop the container
    3. Run coi list
    4. Verify container does not show IPv4 field
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

    time.sleep(2)

    # === Phase 3: Run list ===

    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"

    output = result.stdout

    # === Phase 4: Verify no IPv4 shown for stopped container ===

    assert container_name in output, (
        f"Container {container_name} should appear in list. Got:\n{output}"
    )

    # Extract the section for this specific container
    lines = output.split("\n")
    container_section = []
    in_container = False
    for line in lines:
        if container_name in line:
            in_container = True
        elif in_container and line.strip() and not line.startswith("    "):
            # Next container or section started
            break
        if in_container:
            container_section.append(line)

    container_text = "\n".join(container_section)

    # Should NOT show IPv4 field for stopped container
    assert "IPv4:" not in container_text, (
        f"Should not show IPv4 field for stopped container. Got:\n{container_text}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
