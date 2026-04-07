"""
Test for coi health ufw_conflict check.

Tests that:
1. The ufw_conflict check exists in health output
2. The check returns OK status when ufw is not active
"""

import json
import subprocess


def test_health_ufw_conflict_check_exists(coi_binary):
    """
    Verify that the ufw_conflict check appears in coi health --format json output.
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should succeed (exit 0 for healthy, 1 for degraded)
    assert result.returncode in [0, 1, 2], (
        f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
    )

    data = json.loads(result.stdout)
    checks = data["checks"]
    assert "ufw_conflict" in checks, "Should have 'ufw_conflict' check"

    check = checks["ufw_conflict"]
    assert check["name"] == "ufw_conflict", "Check name should be 'ufw_conflict'"
    assert check["status"] in ["ok", "warning", "failed"], f"Invalid status: {check['status']}"


def test_health_ufw_conflict_status_ok_when_ufw_inactive(coi_binary):
    """
    Verify OK status on systems without active ufw.

    On most CI and dev systems ufw is either not installed or inactive,
    so this check should return OK.
    """
    # First check if ufw is actually active on this system (no sudo needed)
    ufw_result = subprocess.run(
        ["systemctl", "is-active", "--quiet", "ufw"],
        capture_output=True,
        timeout=10,
    )
    ufw_active = ufw_result.returncode == 0

    if ufw_active:
        # If ufw IS active on this system, we can't assert OK — skip
        import pytest

        pytest.skip("ufw is active on this system, cannot test OK status")

    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2], (
        f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
    )

    data = json.loads(result.stdout)
    check = data["checks"]["ufw_conflict"]
    assert check["status"] == "ok", (
        f"Expected OK status when ufw is inactive, got: {check['status']} — {check['message']}"
    )
