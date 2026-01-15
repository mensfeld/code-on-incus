"""
Test for coi build --help - help text validation.

Tests that:
1. Run coi build --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_build_help(coi_binary):
    """
    Test build command help output.

    Flow:
    1. Run coi build --help
    2. Verify exit code is 0
    3. Verify output contains usage and key flags
    """
    result = subprocess.run(
        [coi_binary, "build", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Build help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the build command
    assert "build" in output.lower(), f"Should mention build command. Got:\n{output}"

    # Should mention image building
    assert "image" in output.lower(), f"Should mention image building. Got:\n{output}"

    # Should document force flag
    assert "--force" in output, f"Should document --force flag. Got:\n{output}"
