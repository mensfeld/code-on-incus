"""
Integration tests for custom image building.

Tests:
- coi build custom with script
- Custom image with base specified
- Custom image with privileged base
"""

import subprocess


def test_build_custom_script_not_found(coi_binary):
    """Test that build fails with nonexistent script."""
    result = subprocess.run(
        [coi_binary, "build", "custom", "test-image", "--script", "/nonexistent/script.sh"],
        capture_output=True,
        text=True,
    )
    assert result.returncode != 0, "Build should fail with nonexistent script"
    assert "not found" in result.stderr.lower()
