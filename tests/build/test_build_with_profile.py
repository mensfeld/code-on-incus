"""
Test build with --profile flag.

Tests that:
1. coi build --profile nonexistent produces a clear error
"""

import subprocess


def test_build_profile_not_found(coi_binary, workspace_dir):
    """
    Test that coi build --profile nonexistent fails with a clear
    error message mentioning the profile was not found.
    """
    result = subprocess.run(
        [
            coi_binary,
            "build",
            "--profile",
            "nonexistent",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, (
        f"Build with nonexistent profile should fail. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention profile not found. Got:\n{combined}"
    )
    assert "nonexistent" in combined, f"Error should mention the profile name. Got:\n{combined}"
