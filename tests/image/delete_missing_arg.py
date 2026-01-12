"""
Test for coi image delete - missing argument.

Tests that:
1. Run coi image delete without image name
2. Verify it shows usage error
"""

import subprocess


def test_delete_missing_arg(coi_binary, cleanup_containers):
    """
    Test that coi image delete without argument shows error.

    Flow:
    1. Run coi image delete (no alias)
    2. Verify it fails with usage message
    """
    # === Phase 1: Run without argument ===

    result = subprocess.run(
        [coi_binary, "image", "delete"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # === Phase 2: Verify failure ===

    assert result.returncode != 0, f"Missing argument should fail. stdout: {result.stdout}"

    combined_output = (result.stdout + result.stderr).lower()
    assert (
        "usage" in combined_output or "required" in combined_output or "argument" in combined_output
    ), f"Should show usage or argument error. Got:\n{result.stdout + result.stderr}"
