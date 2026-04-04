"""
Test that CLI Use strings follow the <required> / [optional] lowercase-kebab-case convention.

Issue #12: Use strings mixed UPPERCASE, <angle-bracket>, and [bracket] styles.
"""

import subprocess


def test_tmux_send_help_uses_angle_brackets(coi_binary):
    """coi tmux send --help should show <session-name> and <command>."""
    result = subprocess.run(
        [coi_binary, "tmux", "send", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "<session-name>" in combined, (
        f"Expected '<session-name>' in help output, got: {combined}"
    )
    assert "<command>" in combined, f"Expected '<command>' in help output, got: {combined}"


def test_tmux_capture_help_uses_angle_brackets(coi_binary):
    """coi tmux capture --help should show <session-name>."""
    result = subprocess.run(
        [coi_binary, "tmux", "capture", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "<session-name>" in combined, (
        f"Expected '<session-name>' in help output, got: {combined}"
    )


def test_info_help_uses_lowercase(coi_binary):
    """coi info --help should show [session-id] not [SESSION_ID]."""
    result = subprocess.run(
        [coi_binary, "info", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "[session-id]" in combined, f"Expected '[session-id]' in help output, got: {combined}"
    assert "SESSION_ID" not in combined, (
        f"Old 'SESSION_ID' style should not appear, got: {combined}"
    )


def test_run_help_uses_angle_brackets(coi_binary):
    """coi run --help should show <command> [args...] not COMMAND."""
    result = subprocess.run(
        [coi_binary, "run", "--help"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Expected exit 0, got {result.returncode}: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "<command>" in combined, f"Expected '<command>' in help output, got: {combined}"
    assert "[args...]" in combined, f"Expected '[args...]' in help output, got: {combined}"
