"""
Test that coi update core --check displays current and latest version numbers.
"""

import subprocess


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

    assert result.returncode == 0, f"Update check should succeed. stderr: {result.stderr}"

    output = result.stdout
    assert "Current version:" in output, f"Should show current version. Got:\n{output}"
    assert "Latest release:" in output, f"Should show latest release. Got:\n{output}"
