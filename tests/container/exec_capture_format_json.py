"""Test coi container exec --capture (JSON format, default)"""

import json
import subprocess

from support.helpers import calculate_container_name


def test_exec_capture_format_json(coi_binary, cleanup_containers, workspace_dir):
    """Test that --capture outputs JSON by default."""
    container_name = calculate_container_name(workspace_dir, 1)

    # Launch container
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0

    # Execute command with capture (no format flag)
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            container_name,
            "--",
            "echo",
            "test output",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Command failed: {result.stderr}"

    # Parse JSON
    data = json.loads(result.stdout)

    # Verify structure
    assert "stdout" in data, "Missing stdout field"
    assert "stderr" in data, "Missing stderr field"
    assert "exit_code" in data, "Missing exit_code field"

    # Verify values
    assert data["stdout"] == "test output\n", f"Unexpected stdout: {data['stdout']}"
    assert data["exit_code"] == 0, "Exit code should be 0"

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
