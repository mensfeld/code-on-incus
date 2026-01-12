"""
Test for coi list - no containers running.

Tests that:
1. Run coi list when no containers exist
2. Verify it shows "(none)" or empty list
"""

import subprocess


def test_list_no_containers(coi_binary, cleanup_containers):
    """
    Test coi list when no containers are running.

    Flow:
    1. Run coi list
    2. Verify it shows Active Containers section
    3. Verify it handles empty state gracefully

    Note: Other tests may have containers running, so we check for valid output format.
    """
    result = subprocess.run(
        [coi_binary, "list"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"List should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should always show Active Containers header
    assert "Active Containers:" in output, f"Should show Active Containers section. Got:\n{output}"

    # Either shows "(none)" or actual containers - both valid
    assert "(none)" in output or "coi-" in output or "Status:" in output, (
        f"Should show containers or (none). Got:\n{output}"
    )
