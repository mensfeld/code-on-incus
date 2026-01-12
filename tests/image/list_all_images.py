"""
Test for coi image list --all - list all local images.

Tests that:
1. Run coi image list --all
2. Verify it shows all local images
"""

import subprocess


def test_list_all_images(coi_binary, cleanup_containers):
    """
    Test listing all local images with --all flag.

    Flow:
    1. Run coi image list --all
    2. Verify output contains All Local Images section
    """
    # === Phase 1: Run image list --all ===

    result = subprocess.run(
        [coi_binary, "image", "list", "--all"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Image list --all should succeed. stderr: {result.stderr}"

    # === Phase 2: Verify output format ===

    combined_output = result.stdout + result.stderr

    # Should show All Local Images section
    assert "All Local Images:" in combined_output or "ALIAS" in combined_output, (
        f"Should show All Local Images section or header. Got:\n{combined_output}"
    )
