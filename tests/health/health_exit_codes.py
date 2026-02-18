"""
Test for coi health exit codes.

Tests that:
1. Exit code 0 for healthy
2. Exit code 1 for degraded (warnings)
3. Exit code 2 for unhealthy (failures)

Note: We can only test the healthy case reliably in CI since we expect
the environment to be properly configured.
"""

import json
import subprocess


def test_health_exit_code_matches_status(coi_binary):
    """
    Test that exit code matches reported status.

    Flow:
    1. Run coi health --format json
    2. Parse the status field
    3. Verify exit code matches status
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Parse JSON to get status
    data = json.loads(result.stdout)
    status = data["status"]

    # Map status to expected exit code
    expected_exit_codes = {
        "healthy": 0,
        "degraded": 1,
        "unhealthy": 2,
    }

    expected = expected_exit_codes.get(status)
    assert expected is not None, f"Unknown status: {status}"
    assert result.returncode == expected, (
        f"Exit code {result.returncode} doesn't match status '{status}' (expected {expected})"
    )


def test_health_summary_matches_checks(coi_binary):
    """
    Test that summary counts match actual check results.

    Flow:
    1. Run coi health --format json
    2. Count checks by status
    3. Verify summary matches counts
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    data = json.loads(result.stdout)
    checks = data["checks"]
    summary = data["summary"]

    # Count checks by status
    passed = sum(1 for c in checks.values() if c["status"] == "ok")
    warnings = sum(1 for c in checks.values() if c["status"] == "warning")
    failed = sum(1 for c in checks.values() if c["status"] == "failed")
    total = len(checks)

    # Verify summary matches
    assert summary["total"] == total, f"Total mismatch: {summary['total']} != {total}"
    assert summary["passed"] == passed, f"Passed mismatch: {summary['passed']} != {passed}"
    assert summary["warnings"] == warnings, (
        f"Warnings mismatch: {summary['warnings']} != {warnings}"
    )
    assert summary["failed"] == failed, f"Failed mismatch: {summary['failed']} != {failed}"
