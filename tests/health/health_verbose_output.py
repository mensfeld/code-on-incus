"""
Test for coi health --verbose - verbose output with additional checks.

Tests that:
1. Verbose flag adds additional checks
2. DNS resolution check is included
3. Passwordless sudo check is included
"""

import subprocess


def test_health_verbose_output(coi_binary):
    """
    Test health command with verbose flag.

    Flow:
    1. Run coi health --verbose
    2. Verify additional checks appear (DNS, sudo)
    3. Verify OPTIONAL section exists
    """
    result = subprocess.run(
        [coi_binary, "health", "--verbose"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should succeed (exit 0 for healthy, 1 for degraded)
    assert result.returncode in [0, 1], (
        f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
    )

    output = result.stdout

    # Verify OPTIONAL section exists with verbose
    assert "OPTIONAL:" in output, "Verbose should include OPTIONAL section"

    # Verify DNS check appears
    assert "DNS resolution" in output, "Verbose should check DNS resolution"

    # Verify passwordless sudo check appears
    assert "Passwordless sudo" in output or "sudo" in output.lower(), (
        "Verbose should check passwordless sudo"
    )
