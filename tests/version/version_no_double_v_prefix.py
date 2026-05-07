"""
Test for coi version - no double v prefix regression.

Tests that:
1. Run coi version
2. Verify version line does NOT contain "vv" (double v prefix)
3. Run coi update --check
4. Verify "Current version:" line does NOT contain "vv"
"""

import subprocess


def test_version_no_double_v(coi_binary):
    """
    Regression test: version output must not show "vv" prefix.

    git describe --tags produces a v-prefixed string (e.g. v0.8.1).
    The Go code adds another "v" for display. If the Makefile does not
    strip the leading v, the output becomes "vv0.8.1".

    Flow:
    1. Run coi version
    2. Assert output does not contain "vv"
    """
    result = subprocess.run(
        [coi_binary, "version"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Version command should succeed. stderr: {result.stderr}"
    assert "vv" not in result.stdout, (
        f"Version output must not contain double 'vv' prefix. Got:\n{result.stdout}"
    )


def test_update_check_no_double_v(coi_binary):
    """
    Regression test: update --check must not show "vv" in current version.

    Flow:
    1. Run coi update --check
    2. Assert "Current version:" line does not contain "vv"
    """
    result = subprocess.run(
        [coi_binary, "update", "--check"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"Update check should succeed. stderr: {result.stderr}"

    found = False
    for line in result.stdout.splitlines():
        if "Current version:" in line:
            found = True
            assert "vv" not in line, (
                f"Current version must not contain double 'vv' prefix. Got: {line}"
            )
            break

    assert found, f"Expected 'Current version:' line in output. Got:\n{result.stdout}"
