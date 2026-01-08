"""
Test help text shows common commands.

Expected:
- Help mentions available commands like shell, list, attach
"""

import subprocess


def test_help_shows_common_commands(coi_binary):
    """Test that help text mentions common commands."""
    result = subprocess.run([coi_binary, "--help"], capture_output=True, text=True, timeout=5)

    assert result.returncode == 0

    # Check for common commands in help
    common_commands = ["shell", "list", "attach", "build", "images"]
    output = result.stdout.lower()

    found_commands = [cmd for cmd in common_commands if cmd in output]
    assert len(found_commands) >= 3, (
        f"Expected at least 3 common commands in help, found: {found_commands}"
    )
