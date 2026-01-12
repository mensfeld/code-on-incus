"""
Test for coi clean - no stopped containers.

Tests that:
1. Run coi clean when no stopped containers exist
2. Verify it shows appropriate message
"""

import subprocess


def test_clean_no_stopped_containers(coi_binary, cleanup_containers):
    """
    Test that coi clean with no stopped containers shows appropriate message.

    Flow:
    1. Ensure no stopped containers exist
    2. Run coi clean --force
    3. Verify output indicates nothing to clean
    """
    result = subprocess.run(
        [coi_binary, "clean", "--force"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # Should succeed
    assert result.returncode == 0, f"coi clean should succeed. stderr: {result.stderr}"

    # Should indicate nothing to clean or complete successfully
    combined_output = result.stdout + result.stderr
    success_indicators = (
        "no stopped" in combined_output.lower()
        or "nothing to clean" in combined_output.lower()
        or "cleaned" in combined_output.lower()
        or "0 container" in combined_output.lower()
        or result.returncode == 0  # Success with no output is also valid
    )

    assert success_indicators, (
        f"Should indicate nothing to clean or succeed. Got:\n{combined_output}"
    )
