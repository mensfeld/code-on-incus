"""Test coi container exec --capture --format=raw"""

import subprocess

from support.helpers import calculate_container_name


def test_exec_capture_format_raw(coi_binary, cleanup_containers, workspace_dir):
    """Test that --capture --format=raw outputs raw stdout."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Phase 1: Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Phase 2: Execute command with raw format
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            "--format=raw",
            container_name,
            "--",
            "echo",
            "hello world",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Verify raw output
    assert result.returncode == 0, "Command should succeed"
    assert result.stdout == "hello world\n", f"Expected 'hello world\\n', got '{result.stdout}'"

    # Should NOT be JSON
    assert not result.stdout.strip().startswith("{"), "Should not output JSON"

    # Phase 3: Test command failure
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            "--format=raw",
            container_name,
            "--",
            "false",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Verify exit code propagation
    assert result.returncode == 1, "Should exit with code 1 for failed command"

    # Phase 4: Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
