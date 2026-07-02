"""
Test for persistent coi run - no spurious errors during cleanup.

Tests that:
1. Run with [container] persistent = true config
2. Container stops itself (command completes)
3. Cleanup does not show "Error: The instance is already stopped"

This test validates the fix for the issue where cleanup tried to stop an
already-stopped persistent container, causing spurious error messages.
"""

import subprocess

from support.helpers import calculate_container_name, write_workspace_container_config


def test_run_persistent_no_spurious_errors(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that persistent run cleanup doesn't show spurious stop errors.

    Flow:
    1. Run coi run with persistent config and a simple command
    2. Command completes and container stops itself
    3. Verify no "Error: The instance is already stopped" message
    4. Cleanup
    """
    slot = 9
    container_name = calculate_container_name(workspace_dir, slot)
    write_workspace_container_config(workspace_dir, persistent=True)

    # Run with persistent - container will stop after command completes
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot",
            str(slot),
            "echo",
            "test-persistent-cleanup",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    # Command should succeed
    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr

    # Verify command output is present
    assert "test-persistent-cleanup" in combined_output, (
        f"Output should contain echo text. Got:\n{combined_output}"
    )

    # Verify no spurious error about already stopped container
    # This error was appearing during cleanup when the container had already
    # stopped itself after the command completed
    assert "Error: The instance is already stopped" not in combined_output, (
        f"Should not show spurious 'already stopped' error. Output:\n{combined_output}"
    )

    # Also check for any "Error:" in output during successful runs
    # Success messages are ok, but "Error:" should only appear in actual failures
    if result.returncode == 0:
        error_lines = [
            line
            for line in combined_output.split("\n")
            if "Error:" in line and "already stopped" in line.lower()
        ]
        assert len(error_lines) == 0, (
            "Successful run should not contain error messages. Found:\n" + "\n".join(error_lines)
        )

    # Cleanup
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
