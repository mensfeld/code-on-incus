"""Test that invalid --format values are rejected"""

import subprocess

from support.helpers import calculate_container_name


def test_exec_invalid_format(coi_binary, cleanup_containers, workspace_dir):
    """Test that invalid format values are rejected."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Try to use invalid format value (should fail)
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            "--format=xml",
            container_name,
            "--",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail with error about invalid format
    assert result.returncode != 0, "Should fail with invalid format value"
    assert "invalid format" in result.stderr.lower(), "Should show format validation error"
    assert "xml" in result.stderr, "Should mention the invalid format value"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
