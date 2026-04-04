"""
Test that --format flag values are consistent across all commands.

Issue #5: image list used --format table|json while all others use text|json.
          monitor used boolean --json instead of --format.
"""

import subprocess


def test_image_list_format_text(coi_binary):
    """coi image list --format text should succeed (renamed from 'table')."""
    result = subprocess.run(
        [coi_binary, "image", "list", "--format", "text"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed (exit 0) — format is valid
    assert result.returncode == 0, (
        f"Expected exit 0 for --format text, got {result.returncode}: {result.stderr}"
    )


def test_image_list_format_table_rejected(coi_binary):
    """coi image list --format table should now fail with exit code 2."""
    result = subprocess.run(
        [coi_binary, "image", "list", "--format", "table"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, (
        f"Expected exit code 2 for --format table, got {result.returncode}"
    )
    combined = result.stdout + result.stderr
    assert "invalid format" in combined.lower(), f"Expected 'invalid format' error, got: {combined}"


def test_image_list_format_invalid_rejected(coi_binary):
    """coi image list --format xml should fail with exit code 2."""
    result = subprocess.run(
        [coi_binary, "image", "list", "--format", "xml"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, f"Expected exit code 2 for --format xml, got {result.returncode}"
    combined = result.stdout + result.stderr
    assert "invalid format" in combined.lower(), f"Expected 'invalid format' error, got: {combined}"


def test_monitor_format_flag_accepted(coi_binary):
    """coi monitor --format json should be a recognized flag.

    The command will fail because no container exists, but the error
    should NOT be about an unknown flag.
    """
    result = subprocess.run(
        [coi_binary, "monitor", "--format", "json", "nonexistent-container-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr
    # Should NOT fail with "unknown flag"
    assert "unknown flag" not in combined.lower(), (
        f"--format flag should be recognized, got: {combined}"
    )


def test_monitor_json_flag_still_works(coi_binary):
    """coi monitor --json should still be accepted (backward compat)."""
    result = subprocess.run(
        [coi_binary, "monitor", "--json", "nonexistent-container-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr
    # Should NOT fail with "unknown flag"
    assert "unknown flag" not in combined.lower(), (
        f"--json flag should still be recognized, got: {combined}"
    )


def test_monitor_format_invalid_rejected(coi_binary):
    """coi monitor --format xml should fail with validation error."""
    result = subprocess.run(
        [coi_binary, "monitor", "--format", "xml", "nonexistent-container-xyz"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, f"Expected exit code 2 for --format xml, got {result.returncode}"
    combined = result.stdout + result.stderr
    assert "invalid format" in combined.lower(), f"Expected 'invalid format' error, got: {combined}"
