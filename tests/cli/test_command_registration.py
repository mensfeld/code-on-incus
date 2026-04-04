"""
Test that commands previously self-registering are still visible after centralization.

Issue #15: attach, shutdown, monitor self-registered via rootCmd.AddCommand() in their own init()
while all other commands register centrally in root.go.
"""

import subprocess


def test_attach_in_help(coi_binary):
    """coi --help should list the attach command."""
    result = subprocess.run(
        [coi_binary, "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "attach" in combined, f"Expected 'attach' in help output, got: {combined}"


def test_shutdown_in_help(coi_binary):
    """coi --help should list the shutdown command."""
    result = subprocess.run(
        [coi_binary, "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "shutdown" in combined, f"Expected 'shutdown' in help output, got: {combined}"


def test_monitor_in_help(coi_binary):
    """coi --help should list the monitor command."""
    result = subprocess.run(
        [coi_binary, "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "monitor" in combined, f"Expected 'monitor' in help output, got: {combined}"
