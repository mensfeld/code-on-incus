"""
Test for coi persist - error handling for nonexistent container.

Tests that:
1. Run coi persist on nonexistent container
2. Verify error/warning about container not existing
3. Verify appropriate exit code or warning message
"""

import subprocess


def test_persist_nonexistent_container(coi_binary):
    """
    Test persist command with nonexistent container.

    Flow:
    1. Run persist on a container that doesn't exist
    2. Verify warning/error message
    3. Verify command behavior
    """
    nonexistent_container = "coi-test-nonexistent-xyz123"

    # === Phase 1: Try to persist nonexistent container ===

    result = subprocess.run(
        [coi_binary, "persist", nonexistent_container],
        capture_output=True,
        text=True,
        timeout=60,
    )

    # The command should either:
    # 1. Exit with non-zero status (error)
    # 2. OR show a warning and continue (warning mode)

    combined_output = result.stdout + result.stderr

    # Should mention the container doesn't exist
    assert (
        "does not exist" in combined_output.lower()
        or "not found" in combined_output.lower()
        or "warning" in combined_output.lower()
    ), f"Should show error/warning about container not existing. Got:\n{combined_output}"

    # If it exits with error, that's acceptable
    # If it shows warning but exits 0, that's also acceptable (warn and continue pattern)
    # But it should NOT silently succeed
    assert result.returncode != 0 or "warning" in combined_output.lower(), (
        "Should either error or show warning for nonexistent container"
    )
