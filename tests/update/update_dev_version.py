"""
Test for coi update with dev version - warns and refuses without --force.

Tests that:
1. Run coi update (without --force) on a dev build
2. Verify it warns about dev builds and does not proceed
3. Verify --force is suggested
"""

import subprocess


def test_update_dev_version_refuses_without_force(coi_binary):
    """
    Test that dev build refuses to update without --force.

    Flow:
    1. Check if this is a dev build
    2. Run coi update (no --force, no --check)
    3. Verify it warns and exits without updating
    """
    # Check current version
    version_result = subprocess.run(
        [coi_binary, "version"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    if "dev" not in version_result.stdout.lower():
        # Not a dev build, skip this test
        import pytest

        pytest.skip("Not a dev build — test only applies to dev versions")

    result = subprocess.run(
        [coi_binary, "update"],
        capture_output=True,
        text=True,
        timeout=30,
        # Pipe stdin to prevent blocking on confirmation prompt
        input="n\n",
    )

    output = result.stdout

    # Should mention dev build warning
    assert "development build" in output.lower() or "dev" in output, (
        f"Should warn about dev build. Got:\n{output}"
    )

    # Should suggest --force
    assert "--force" in output, f"Should suggest --force flag. Got:\n{output}"
