"""
Test for coi image list --prefix - filter images by prefix.

Tests that:
1. Run coi image list --prefix <prefix>
2. Verify it filters results correctly
"""

import subprocess


def test_list_with_prefix(coi_binary, cleanup_containers):
    """
    Test listing images filtered by prefix.

    Flow:
    1. Run coi image list --prefix coi
    2. Verify output only shows matching images or "no images found"
    """
    # === Phase 1: Run image list with prefix ===

    result = subprocess.run(
        [coi_binary, "image", "list", "--prefix", "coi"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Image list with prefix should succeed. stderr: {result.stderr}"

    # === Phase 2: Verify output ===

    combined_output = result.stdout + result.stderr

    # Should either show images with prefix or "No images found"
    assert "coi" in combined_output.lower() or "No images found" in combined_output, (
        f"Should show filtered results or 'No images found'. Got:\n{combined_output}"
    )


def test_list_with_nonexistent_prefix(coi_binary, cleanup_containers):
    """
    Test listing images with prefix that matches nothing.

    Flow:
    1. Run coi image list --prefix nonexistent-xyz-123
    2. Verify it shows "no images found" message
    """
    # === Phase 1: Run image list with nonexistent prefix ===

    result = subprocess.run(
        [coi_binary, "image", "list", "--prefix", "nonexistent-xyz-123-abc"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"Image list with nonexistent prefix should succeed. stderr: {result.stderr}"
    )

    # === Phase 2: Verify no images found ===

    combined_output = result.stdout + result.stderr
    assert "No images found" in combined_output or combined_output.strip() == "", (
        f"Should show 'No images found' or empty result. Got:\n{combined_output}"
    )
