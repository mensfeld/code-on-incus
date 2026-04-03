"""
Test that CLI error handling works correctly after removing exitError()/os.Exit()
and replacing with proper cobra error returns.

Bug #13: exitError() called os.Exit() directly, bypassing cobra error handling
and skipping deferred cleanup (container deletion, firewall teardown).
"""

import subprocess


def test_container_launch_nonexistent_returns_error(coi_binary):
    """coi container launch with a nonexistent image should return non-zero
    exit code with an error message through the cobra error path.

    This verifies the exitError -> fmt.Errorf migration works correctly.
    """
    result = subprocess.run(
        [coi_binary, "container", "launch", "nonexistent-image-xyz", "test-err-name"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, "Should fail with nonexistent image"

    combined = result.stdout + result.stderr
    assert "failed to launch container" in combined.lower() or "error" in combined.lower(), (
        f"Expected error message about failed launch, got: {combined}"
    )


def test_container_exec_missing_command_returns_error(coi_binary):
    """coi container exec with no command should return a usage/validation error."""
    result = subprocess.run(
        [coi_binary, "container", "exec", "fake-container"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail — either because args validation or because no command
    assert result.returncode != 0, "Should fail with missing command"


def test_health_check_returns_exit_code(coi_binary):
    """coi health should return an appropriate exit code based on system state.

    After the fix, health returns ExitCodeError instead of calling os.Exit(),
    so the exit code is propagated through cobra to main.go.
    """
    result = subprocess.run(
        [coi_binary, "health"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    combined = result.stdout + result.stderr

    # Health check should produce output regardless of pass/fail
    assert "health check" in combined.lower() or "status" in combined.lower(), (
        f"Expected health check output, got: {combined}"
    )

    # Exit code should be 0, 1, or 2 (healthy, degraded, unhealthy)
    assert result.returncode in (0, 1, 2), f"Expected exit code 0/1/2, got {result.returncode}"


def test_image_exists_nonexistent_returns_exit_code_1(coi_binary):
    """coi image exists <nonexistent> should exit with code 1 (no error message).

    This tests the ExitCodeError path for silent boolean-check commands.
    """
    result = subprocess.run(
        [coi_binary, "image", "exists", "nonexistent-image-xyz-999"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode != 0, "Should return non-zero for nonexistent image"
    # Silent boolean-check command: no output expected
    assert result.stdout.strip() == "", (
        f"Expected no stdout for nonexistent image exists check, got: {result.stdout!r}"
    )
    assert result.stderr.strip() == "", (
        f"Expected no stderr for nonexistent image exists check, got: {result.stderr!r}"
    )
