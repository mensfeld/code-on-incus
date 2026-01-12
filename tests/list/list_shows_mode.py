"""
Test for coi list - shows ephemeral/persistent mode.

Tests that:
1. Launch a container
2. Run coi list
3. Verify it shows (ephemeral) or (persistent) indicator
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_list_shows_mode(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi list shows ephemeral/persistent mode.

    Flow:
    1. Launch a container
    2. Run coi list
    3. Verify mode indicator appears
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

    # === Phase 2: Run list ===

    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"

    output = result.stdout

    # === Phase 3: Verify mode indicator ===

    assert container_name in output, f"Container should appear. Got:\n{output}"

    # Should show either (ephemeral) or (persistent)
    assert "(ephemeral)" in output or "(persistent)" in output, (
        f"Should show mode indicator. Got:\n{output}"
    )

    # === Phase 4: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
