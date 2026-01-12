"""
Test for coi container launch - fails with missing arguments.

Tests that:
1. Run coi container launch without required arguments
2. Verify command fails with usage help
"""

import subprocess


def test_launch_missing_args(coi_binary, cleanup_containers):
    """
    Test that launch without required arguments shows usage.

    Flow:
    1. Run coi container launch with no arguments
    2. Verify it fails
    3. Verify usage information is shown
    """
    # === Phase 1: Run launch without arguments ===

    result = subprocess.run(
        [coi_binary, "container", "launch"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure and usage shown ===

    assert result.returncode != 0, "Launch without arguments should fail"

    combined_output = result.stdout + result.stderr
    has_usage = (
        "usage" in combined_output.lower()
        or "required" in combined_output.lower()
        or "argument" in combined_output.lower()
        or "image" in combined_output.lower()
    )

    assert has_usage, f"Should show usage or argument error. Got:\n{combined_output}"
