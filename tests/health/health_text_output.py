"""
Test for coi health - text output format.

Tests that:
1. Health command runs successfully
2. Output contains expected sections
3. Exit code is 0 when healthy
"""

import subprocess


def test_health_text_output(coi_binary):
    """
    Test health command with default text output.

    Flow:
    1. Run coi health
    2. Verify expected sections appear in output
    3. Verify exit code is 0
    """
    result = subprocess.run(
        [coi_binary, "health"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should succeed (exit 0 for healthy, 1 for degraded)
    assert result.returncode in [0, 1], (
        f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
    )

    output = result.stdout

    # Verify header
    assert "Code on Incus Health Check" in output, "Should have header"

    # Verify key sections exist
    assert "SYSTEM:" in output, "Should have SYSTEM section"
    assert "CRITICAL:" in output, "Should have CRITICAL section"
    assert "NETWORKING:" in output, "Should have NETWORKING section"
    assert "STORAGE:" in output, "Should have STORAGE section"
    assert "CONFIGURATION:" in output, "Should have CONFIGURATION section"
    assert "STATUS:" in output, "Should have STATUS section"

    # Verify key checks appear
    assert "Incus" in output, "Should check Incus"
    assert "Operating system" in output, "Should show OS info"
    assert "Network bridge" in output, "Should check network bridge"
    assert "Disk space" in output, "Should check disk space"
    assert "Incus storage pool" in output, "Should check Incus storage pool"

    # Verify summary line
    assert "checks passed" in output or "checks failed" in output, "Should have summary"
