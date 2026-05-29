"""
Tests for the `coi logs` command.

`coi logs <container> [-f]` tails ~/.coi/logs/<container>.stdout.log and
~/.coi/logs/<container>.stderr.log. These tests verify the command is
registered, its flags are correct, and it handles missing containers gracefully.
No container startup is required.
"""

import subprocess


def test_logs_command_in_help(coi_binary):
    """coi --help should list the logs command."""
    result = subprocess.run(
        [coi_binary, "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "logs" in combined, f"Expected 'logs' in help output, got:\n{combined}"


def test_logs_help_output(coi_binary):
    """coi logs --help should describe the command and its flags."""
    result = subprocess.run(
        [coi_binary, "logs", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "logs" in combined.lower(), f"Expected usage info in help, got:\n{combined}"


def test_logs_follow_flag_in_help(coi_binary):
    """coi logs --help should show -f, --follow flag."""
    result = subprocess.run(
        [coi_binary, "logs", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "--follow" in combined, f"Expected '--follow' in help output, got:\n{combined}"
    assert "-f" in combined, f"Expected '-f' short flag in help output, got:\n{combined}"


def test_logs_nonexistent_container_exits_nonzero(coi_binary):
    """coi logs with a nonexistent container name should exit with a non-zero code."""
    result = subprocess.run(
        [coi_binary, "logs", "nonexistent-container-xyz-99"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, (
        f"Expected non-zero exit for nonexistent container, got 0.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
    combined = result.stdout + result.stderr
    assert (
        "not found" in combined.lower() or "error" in combined.lower() or "no" in combined.lower()
    ), f"Expected error message for nonexistent container, got:\n{combined}"
