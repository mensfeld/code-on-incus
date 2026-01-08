"""
Test help works without Incus access.

Expected:
- Help commands work even without Incus daemon
"""

import subprocess


def test_help_without_incus_access(coi_binary):
    """Test that help commands work even without Incus daemon access."""
    # Run without sg - help should still work
    result = subprocess.run(
        [coi_binary, "--help"],
        capture_output=True,
        text=True,
        timeout=5,
        env={"PATH": "/usr/bin:/bin"},  # Minimal environment
    )

    assert result.returncode == 0
    assert "claude-on-incus" in result.stdout.lower()
