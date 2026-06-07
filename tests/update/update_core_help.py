"""
Test that coi update core --help documents its flags correctly.
"""

import subprocess


def test_update_core_help(coi_binary):
    """
    Run coi update core --help and verify flags are documented.

    Flow:
    1. Run coi update core --help
    2. Verify exit code is 0
    3. Verify --check and --force flags are present; hidden --version is absent
    """
    result = subprocess.run(
        [coi_binary, "update", "core", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Update core help should succeed. stderr: {result.stderr}"

    output = result.stdout
    assert "Usage:" in output, f"Should contain Usage section. Got:\n{output}"
    assert "GitHub" in output or "github" in output.lower(), (
        f"Should mention GitHub. Got:\n{output}"
    )
    assert "--check" in output, f"Should document --check flag. Got:\n{output}"
    assert "--force" in output, f"Should document --force flag. Got:\n{output}"
    assert "--version" not in output, (
        f"Hidden --version flag should not appear in help. Got:\n{output}"
    )
