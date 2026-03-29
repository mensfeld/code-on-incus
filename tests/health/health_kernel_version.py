"""
Test for coi health — kernel version check appears and reports correctly.

Tests that:
1. Kernel version check appears in text output
2. Kernel version check appears in JSON output with correct structure
3. On a modern kernel (>= 5.15), status is "ok"
"""

import json
import subprocess


def test_health_kernel_version_text(coi_binary):
    """
    Verify coi health text output includes the kernel version check.
    """
    result = subprocess.run(
        [coi_binary, "health"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1], (
        f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
    )

    output = result.stdout
    assert "Kernel version" in output, (
        f"Should show Kernel version check in text output. Got:\n{output}"
    )


def test_health_kernel_version_json(coi_binary):
    """
    Verify coi health JSON output includes kernel_version with correct structure.
    On a modern kernel, status should be "ok" and details should include version info.
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1], (
        f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
    )

    data = json.loads(result.stdout)
    checks = data["checks"]

    assert "kernel_version" in checks, "Should have kernel_version check"

    kv = checks["kernel_version"]
    assert kv["status"] == "ok", (
        f"On a modern kernel, kernel_version should be 'ok'. Got: {kv['status']} — {kv['message']}"
    )
    assert "details" in kv, "kernel_version check should include details"
    assert "kernel" in kv["details"], "details should include 'kernel' field"
    assert "major" in kv["details"], "details should include 'major' field"
    assert "minor" in kv["details"], "details should include 'minor' field"
    assert kv["details"]["meets_minimum"] is True, (
        f"Modern kernel should meet minimum. Got: {kv['details']}"
    )
