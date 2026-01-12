"""Test that invalid --format values are rejected for list command"""

import subprocess


def test_list_invalid_format(coi_binary):
    """Test that invalid format values are rejected for list command."""

    # Try to use invalid format value (should fail)
    result = subprocess.run(
        [coi_binary, "list", "--format=xml"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should fail with error about invalid format
    assert result.returncode != 0, "Should fail with invalid format value"
    assert "invalid format" in result.stderr.lower(), "Should show format validation error"
    assert "xml" in result.stderr, "Should mention the invalid format value"
