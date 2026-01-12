"""
Test for coi shutdown --timeout flag.

Tests that:
1. Launch a container
2. Shutdown with custom timeout
3. Verify timeout value is shown in output
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_shutdown_with_timeout(coi_binary, cleanup_containers, workspace_dir):
    """
    Test shutting down with custom timeout.

    Flow:
    1. Launch a container
    2. Run coi shutdown --timeout=5 <container>
    3. Verify timeout is shown in output
    """
    slot = 1
    container_name = calculate_container_name(workspace_dir, slot)

    # Launch a container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # Shutdown with custom timeout
    result = subprocess.run(
        [coi_binary, "shutdown", "--timeout=5", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Shutdown should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    # Should show the timeout value (5s)
    assert "5" in combined_output, f"Should show timeout value in output. Got:\n{combined_output}"
    assert "shutdown" in combined_output.lower(), (
        f"Should show shutdown message. Got:\n{combined_output}"
    )
