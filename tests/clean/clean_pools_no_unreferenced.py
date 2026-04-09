"""
Test `coi clean --pools` when all pools are referenced.

If every Incus pool is either the default or referenced by a loaded
profile, the command is a no-op with a clear message.
"""

import subprocess


def test_clean_pools_no_unreferenced(coi_binary):
    """coi clean --pools should be a no-op when nothing is unreferenced."""
    result = subprocess.run(
        [coi_binary, "clean", "--pools", "--force"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"clean --pools should succeed. stderr: {result.stderr}"
    combined = result.stdout + result.stderr
    # When no unreferenced pools are found, the command should print a clear
    # marker and not delete anything.
    assert "no unreferenced pools" in combined.lower() or "nothing to clean" in combined.lower(), (
        f"Should print no-op message. Got:\n{combined}"
    )
