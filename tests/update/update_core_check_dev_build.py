"""
Test that coi update core --check shows the dev build indicator on a dev binary.
"""

import subprocess

import pytest


def test_update_check_dev_build(coi_binary):
    """
    When the binary is a dev build, --check should surface the dev indicator.

    Flow:
    1. Run coi version to detect whether this is a dev build
    2. Run coi update core --check
    3. If dev build, verify the output contains the dev indicator
    """
    version_result = subprocess.run(
        [coi_binary, "version"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    is_dev = "dev" in version_result.stdout.lower()

    result = subprocess.run(
        [coi_binary, "update", "core", "--check"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    # A failed GitHub API call (rate-limit/403, 429, 5xx/504 gateway errors,
    # network/timeout) is an external dependency outside our control in CI; skip
    # rather than fail so the suite stays green. coi prints "failed to check for
    # updates" for any such API error.
    if result.returncode != 0 and "failed to check for updates" in result.stderr.lower():
        pytest.skip(f"GitHub API unavailable: {result.stderr.strip()}")

    assert result.returncode == 0, f"Update check should succeed. stderr: {result.stderr}"

    if is_dev:
        output = result.stdout
        assert "development build" in output.lower() or "dev" in output, (
            f"Dev build should be indicated. Got:\n{output}"
        )
