"""Test that --format requires --capture flag"""

import subprocess

from support.helpers import calculate_container_name


def test_exec_format_requires_capture(coi_binary, cleanup_containers, workspace_dir):
    """Test that --format flag requires --capture flag."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Try to use --format without --capture (should fail)
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--format=raw",
            container_name,
            "--",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail with error about --format requiring --capture
    assert result.returncode != 0, "Should fail when --format used without --capture"
    assert "--format flag requires --capture" in result.stderr, "Should show validation error"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
