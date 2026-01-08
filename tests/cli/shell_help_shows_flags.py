"""
Test shell help shows important flags.

Expected:
- Help shows key flags like --slot, --persistent, --privileged
"""

import subprocess


def test_shell_help_shows_flags(coi_binary):
    """Test that shell --help shows important flags."""
    result = subprocess.run(
        [coi_binary, "shell", "--help"], capture_output=True, text=True, timeout=5
    )

    assert result.returncode == 0
    output = result.stdout.lower()

    # Check for important flags
    important_flags = ["--slot", "--persistent", "--privileged", "--tmux"]
    found_flags = [flag for flag in important_flags if flag in output]

    assert len(found_flags) >= 3, f"Expected important flags in help, found: {found_flags}"
