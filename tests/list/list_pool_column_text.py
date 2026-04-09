"""
Test that `coi list` text output shows a Pool field for running containers.
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_list_pool_column_text(coi_binary, cleanup_containers, workspace_dir):
    """coi list text output should include a Pool field for a running container."""
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
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"list should succeed. stderr: {result.stderr}"

    output = result.stdout
    assert container_name in output, f"Container should appear. Got:\n{output}"
    assert "Pool:" in output, f"List text output should include Pool field. Got:\n{output}"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
