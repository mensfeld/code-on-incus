"""Test that actual exit codes are preserved"""

import subprocess

from support.helpers import calculate_container_name


def test_exec_exit_code_preservation_raw(coi_binary, cleanup_containers, workspace_dir):
    """Test that actual exit codes are preserved in raw format."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Test exit code 2
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            "--format=raw",
            container_name,
            "--",
            "bash",
            "-c",
            "exit 2",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 2, f"Expected exit code 2, got {result.returncode}"

    # Test exit code 42
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            "--format=raw",
            container_name,
            "--",
            "bash",
            "-c",
            "exit 42",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 42, f"Expected exit code 42, got {result.returncode}"

    # Test exit code 127 (command not found)
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            "--format=raw",
            container_name,
            "--",
            "nonexistent_command_xyz",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 127, (
        f"Expected exit code 127 (command not found), got {result.returncode}"
    )

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_exec_exit_code_preservation_json(coi_binary, cleanup_containers, workspace_dir):
    """Test that actual exit codes are preserved in JSON format."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Test exit code 2 in JSON format
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            "--format=json",
            container_name,
            "--",
            "bash",
            "-c",
            "exit 2",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "JSON format should return 0 for coi itself"

    # Parse JSON and check exit_code field
    import json

    data = json.loads(result.stdout)
    assert data["exit_code"] == 2, f"Expected exit_code 2 in JSON, got {data['exit_code']}"

    # Test exit code 42 in JSON format
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            "--format=json",
            container_name,
            "--",
            "bash",
            "-c",
            "exit 42",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    data = json.loads(result.stdout)
    assert data["exit_code"] == 42, f"Expected exit_code 42 in JSON, got {data['exit_code']}"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
