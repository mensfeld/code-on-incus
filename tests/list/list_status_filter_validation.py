"""Test that coi list rejects invalid and conflicting status filter flags."""

import subprocess


def test_list_invalid_status_value(coi_binary):
    """Test that an unknown --status value is rejected with an actionable error."""

    result = subprocess.run(
        [coi_binary, "list", "--status", "paused"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, f"Should exit with code 2, got {result.returncode}"
    assert "invalid status" in result.stderr.lower(), "Should show status validation error"
    assert "paused" in result.stderr, "Should mention the invalid status value"
    assert "running" in result.stderr, "Should list the valid values"


def test_list_explicit_empty_status(coi_binary):
    """Test that an explicitly empty --status is rejected, not silently unfiltered."""

    result = subprocess.run(
        [coi_binary, "list", "--status", ""],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, f"Should exit with code 2, got {result.returncode}"
    assert "invalid status" in result.stderr.lower(), "Should show status validation error"


def test_list_shorthand_empty_status_conflict(coi_binary):
    """Test that --running plus an explicitly empty --status still trips mutual exclusion."""

    result = subprocess.run(
        [coi_binary, "list", "--running", "--status", ""],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, f"Should exit with code 2, got {result.returncode}"
    assert "mutually exclusive" in result.stderr.lower(), (
        "Should explain the flags are mutually exclusive"
    )


def test_list_running_stopped_conflict(coi_binary):
    """Test that --running and --stopped cannot be combined."""

    result = subprocess.run(
        [coi_binary, "list", "--running", "--stopped"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, f"Should exit with code 2, got {result.returncode}"
    assert "mutually exclusive" in result.stderr.lower(), (
        "Should explain the flags are mutually exclusive"
    )


def test_list_shorthand_status_conflict(coi_binary):
    """Test that a shorthand cannot be combined with --status, even consistently."""

    result = subprocess.run(
        [coi_binary, "list", "--running", "--status", "running"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, f"Should exit with code 2, got {result.returncode}"
    assert "mutually exclusive" in result.stderr.lower(), (
        "Should explain the flags are mutually exclusive"
    )
