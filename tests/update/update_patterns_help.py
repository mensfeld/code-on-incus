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
    3. Verify --source, --dry-run, and --gtfobins-dir flags are present
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
    assert "--source" in output, f"Should document --source flag. Got:\n{output}"
    assert "--dry-run" in output, f"Should document --dry-run flag. Got:\n{output}"
    assert "--gtfobins-dir" in output, f"Should document --gtfobins-dir flag. Got:\n{output}"
