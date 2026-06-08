"""
Test that `coi update sigma --help` documents the expected flags.
"""

import subprocess


def test_update_sigma_help(coi_binary):
    """
    coi update sigma --help exits 0 and lists the key flags.
    """
    result = subprocess.run(
        [coi_binary, "update", "sigma", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, f"Expected exit 0. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "Usage:" in output, f"Expected 'Usage:' in output. Got:\n{output}"
    assert "--source" in output, f"Expected '--source' flag. Got:\n{output}"
    assert "--dry-run" in output, f"Expected '--dry-run' flag. Got:\n{output}"
    assert "--sigma-dir" in output, f"Expected '--sigma-dir' flag. Got:\n{output}"
