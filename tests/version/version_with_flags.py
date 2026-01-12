"""
Test for coi version - with help flag.

Tests that:
1. Run coi version --help
2. Verify it shows help text or version info
"""

import subprocess


def test_version_with_help_flag(coi_binary):
    """
    Test version command with --help flag.

    Flow:
    1. Run coi version --help
    2. Verify exit code is 0
    3. Verify output contains help or version information
    """
    result = subprocess.run(
        [coi_binary, "version", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Version --help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should show either help text or version info
    # Cobra typically shows help text for --help flag
    assert len(output) > 0, "Should produce some output"

    # Should mention version command
    assert "version" in output.lower(), f"Output should mention version. Got:\n{output}"
