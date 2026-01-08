"""
Test invalid command handling.

Expected:
- Invalid commands exit with non-zero code
"""

import subprocess


def test_invalid_command_exits_nonzero(coi_binary):
    """Test that invalid commands exit with non-zero code."""
    result = subprocess.run(
        [coi_binary, "nonexistent-command"],
        capture_output=True,
        text=True,
        timeout=5,
    )

    assert result.returncode != 0, "Invalid command should exit with non-zero code"
