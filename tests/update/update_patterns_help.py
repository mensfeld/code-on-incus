"""
Test that coi update patterns --help documents its flags correctly.
"""

import subprocess


def test_update_patterns_help(coi_binary):
    """
    Run coi update patterns --help and verify flags are documented.

    Flow:
    1. Run coi update patterns --help
    2. Verify exit code is 0
    3. Verify --dry-run flag is present and location/source flags are absent
    """
    result = subprocess.run(
        [coi_binary, "update", "patterns", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Update patterns help should succeed. stderr: {result.stderr}"

    output = result.stdout
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"
    assert "--dry-run" in output, f"Should document --dry-run flag. Got:\n{output}"
    assert "GTFOBins" in output, f"Should mention GTFOBins. Got:\n{output}"
    assert "Sigma" in output, f"Should mention Sigma. Got:\n{output}"
    # Location/source are now config-only — no flags for them
    assert "--gtfobins-dir" not in output, f"Should not expose --gtfobins-dir flag. Got:\n{output}"
    assert "--source" not in output, f"Should not expose --source flag. Got:\n{output}"
