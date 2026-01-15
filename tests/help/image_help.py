"""
Test for coi image --help - help text validation.

Tests that:
1. Run coi image --help
2. Verify help text contains expected sections
3. Verify exit code is 0
"""

import subprocess


def test_image_help(coi_binary):
    """
    Test image command help output.

    Flow:
    1. Run coi image --help
    2. Verify exit code is 0
    3. Verify output contains subcommands
    """
    result = subprocess.run(
        [coi_binary, "image", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Image help should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should contain Usage section
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"

    # Should describe the image command
    assert "image" in output.lower(), f"Should mention image command. Got:\n{output}"

    # Should list subcommands
    assert "Available Commands:" in output or "Commands:" in output, (
        f"Should list subcommands. Got:\n{output}"
    )

    # Should mention common image operations
    assert "list" in output.lower() or "exists" in output.lower() or "delete" in output.lower(), (
        f"Should mention image operations. Got:\n{output}"
    )
