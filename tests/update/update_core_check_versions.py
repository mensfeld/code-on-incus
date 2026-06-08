"""
Test that coi update core --check displays current and latest version numbers.
"""

import subprocess

import pytest


def test_update_check_shows_versions(coi_binary):
    """
    Run coi update core --check and verify both version lines appear.

    Flow:
    1. Run coi update core --check
    2. Verify exit code is 0
    3. Verify output shows current version and latest release
    """
    result = subprocess.run(
        [coi_binary, "update", "core", "--check"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # GitHub API rate-limiting (403) or network unavailability is outside our
    # control in CI; skip rather than fail so the suite stays green.
    if result.returncode != 0 and (
        "403" in result.stderr or "rate" in result.stderr.lower() or "network" in result.stderr.lower()
    ):
        pytest.skip(f"GitHub API unavailable: {result.stderr.strip()}")

    assert result.returncode == 0, f"Update check should succeed. stderr: {result.stderr}"

    output = result.stdout
    assert "Current version:" in output, f"Should show current version. Got:\n{output}"
    assert "Latest release:" in output, f"Should show latest release. Got:\n{output}"
