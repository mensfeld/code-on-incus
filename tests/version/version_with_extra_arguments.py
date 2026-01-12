"""
Test for coi version - with extra arguments.

Tests that:
1. Run coi version with extra arguments
2. Verify extra arguments are ignored
3. Verify normal version output
"""

import subprocess


def test_version_with_extra_arguments(coi_binary):
    """
    Test version command with extra arguments.

    Flow:
    1. Run coi version with extra arguments
    2. Verify exit code is 0
    3. Verify output is same as without arguments
    """
    # Get baseline output
    baseline_result = subprocess.run(
        [coi_binary, "version"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    # Run with extra arguments
    result = subprocess.run(
        [coi_binary, "version", "extra", "arg", "another"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, (
        f"Version with extra args should succeed. stderr: {result.stderr}"
    )

    # Extra arguments should be ignored (Cobra behavior)
    assert result.stdout == baseline_result.stdout, (
        f"Output should be identical with or without extra args.\n"
        f"Expected:\n{baseline_result.stdout}\n"
        f"Got:\n{result.stdout}"
    )
