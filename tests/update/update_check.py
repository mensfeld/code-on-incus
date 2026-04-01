"""
Test for coi update --check - version check without installing.

Tests that:
1. Run coi update --check
2. Verify it queries GitHub and reports version info
3. Verify exit code is 0 (no error even though we don't install)
"""

import subprocess


def test_update_check_shows_versions(coi_binary):
    """
    Test that --check shows current and latest version.

    Flow:
    1. Run coi update --check
    2. Verify exit code is 0
    3. Verify output shows current version
    4. Verify output shows latest release version
    """
    result = subprocess.run(
        [coi_binary, "update", "--check"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Update check should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should show current version
    assert "Current version:" in output, f"Should show current version. Got:\n{output}"

    # Should show latest release
    assert "Latest release:" in output, f"Should show latest release. Got:\n{output}"


def test_update_check_dev_build(coi_binary):
    """
    Test that --check with dev build shows dev warning.

    Since the test binary is built locally (likely 'dev' version),
    it should show the development build indicator.

    Flow:
    1. Run coi version to check if this is a dev build
    2. Run coi update --check
    3. If dev build, verify dev-specific output
    """
    # Check current version first
    version_result = subprocess.run(
        [coi_binary, "version"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    is_dev = "dev" in version_result.stdout.lower()

    result = subprocess.run(
        [coi_binary, "update", "--check"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Update check should succeed. stderr: {result.stderr}"

    output = result.stdout

    if is_dev:
        # Dev builds should show development build indicator
        assert "development build" in output.lower() or "dev" in output, (
            f"Dev build should be indicated. Got:\n{output}"
        )
