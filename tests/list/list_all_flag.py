"""
Test for coi list --all - shows saved sessions section.

Tests that:
1. Run coi list --all
2. Verify it shows both Active Containers and Saved Sessions sections
"""

import subprocess


def test_list_all_flag(coi_binary, cleanup_containers):
    """
    Test that coi list --all shows Saved Sessions section.

    Flow:
    1. Run coi list --all
    2. Verify both sections appear
    """
    result = subprocess.run(
        [coi_binary, "list", "--all"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"List --all should succeed. stderr: {result.stderr}"

    output = result.stdout

    # Should show Active Containers section
    assert "Active Containers:" in output, f"Should show Active Containers section. Got:\n{output}"

    # Should show Saved Sessions section
    assert "Saved Sessions:" in output, f"Should show Saved Sessions section. Got:\n{output}"
