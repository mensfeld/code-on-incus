"""
Test for coi clean --force - skips confirmation prompt.

Tests that:
1. Run coi clean --force
2. Verify it completes without requiring input
"""

import subprocess


def test_clean_force_skips_confirmation(coi_binary, cleanup_containers):
    """
    Test that coi clean --force skips confirmation prompt.

    Flow:
    1. Run coi clean --force (no input provided)
    2. Verify it completes successfully without hanging
    """
    # Run with short timeout - if it hangs waiting for input, it will timeout
    result = subprocess.run(
        [coi_binary, "clean", "--force"],
        capture_output=True,
        text=True,
        timeout=30,  # Should complete quickly with --force
    )

    # Should succeed without hanging
    assert result.returncode == 0, f"coi clean --force should succeed. stderr: {result.stderr}"

    # Should not contain prompts for confirmation
    combined_output = result.stdout + result.stderr
    no_prompt = (
        "y/n" not in combined_output.lower()
        and "confirm" not in combined_output.lower()
        and "are you sure" not in combined_output.lower()
    )

    assert no_prompt, f"--force should skip confirmation prompts. Got:\n{combined_output}"
