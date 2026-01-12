"""
Test for coi container exec --env - passes environment variables.

Tests that:
1. Launch a container
2. Execute command with --env flag
3. Verify environment variable is set
"""

import subprocess
import time

from support.helpers import (
    calculate_container_name,
)


def test_exec_with_env(coi_binary, cleanup_containers, workspace_dir):
    """
    Test executing command with environment variables.

    Flow:
    1. Launch a container
    2. Execute printenv with --env MY_VAR=test123
    3. Verify output contains the variable value
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

    # === Phase 2: Execute with --env ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--env",
            "MY_TEST_VAR=test123",
            "--",
            "printenv",
            "MY_TEST_VAR",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Exec with --env should succeed. stderr: {result.stderr}"

    # === Phase 3: Verify environment variable ===

    combined_output = result.stdout + result.stderr
    assert "test123" in combined_output.strip(), (
        f"Environment variable should be set. Got:\n{combined_output}"
    )

    # === Phase 4: Test multiple env vars ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--env",
            "VAR1=value1",
            "--env",
            "VAR2=value2",
            "--",
            "sh",
            "-c",
            "echo $VAR1-$VAR2",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Exec with multiple --env should succeed. stderr: {result.stderr}"
    )

    combined_output = result.stdout + result.stderr
    assert "value1-value2" in combined_output.strip(), (
        f"Both env vars should be set. Got:\n{combined_output}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
