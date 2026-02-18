"""
Test for coi health --format json - JSON output format.

Tests that:
1. Health command with --format json returns valid JSON
2. JSON contains expected structure
3. All required fields are present
"""

import json
import subprocess


def test_health_json_output(coi_binary):
    """
    Test health command with JSON output format.

    Flow:
    1. Run coi health --format json
    2. Verify output is valid JSON
    3. Verify structure contains required fields
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should succeed (exit 0 for healthy, 1 for degraded)
    assert result.returncode in [0, 1], (
        f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
    )

    # Parse JSON
    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError as e:
        raise AssertionError(f"Output is not valid JSON: {e}\nOutput: {result.stdout}")

    # Verify top-level structure
    assert "status" in data, "Should have 'status' field"
    assert "timestamp" in data, "Should have 'timestamp' field"
    assert "checks" in data, "Should have 'checks' field"
    assert "summary" in data, "Should have 'summary' field"

    # Verify status is valid
    assert data["status"] in ["healthy", "degraded", "unhealthy"], (
        f"Invalid status: {data['status']}"
    )

    # Verify summary structure
    summary = data["summary"]
    assert "total" in summary, "Summary should have 'total'"
    assert "passed" in summary, "Summary should have 'passed'"
    assert "warnings" in summary, "Summary should have 'warnings'"
    assert "failed" in summary, "Summary should have 'failed'"

    # Verify checks is a dict with expected checks
    checks = data["checks"]
    assert isinstance(checks, dict), "Checks should be a dictionary"

    # Verify some key checks exist
    expected_checks = [
        "os",
        "incus",
        "permissions",
        "image",
        "network_bridge",
        "disk_space",
        "incus_storage_pool",
    ]
    for check_name in expected_checks:
        assert check_name in checks, f"Should have '{check_name}' check"

    # Verify check structure
    for check_name, check_data in checks.items():
        assert "name" in check_data, f"Check '{check_name}' should have 'name'"
        assert "status" in check_data, f"Check '{check_name}' should have 'status'"
        assert "message" in check_data, f"Check '{check_name}' should have 'message'"
        assert check_data["status"] in ["ok", "warning", "failed"], (
            f"Check '{check_name}' has invalid status: {check_data['status']}"
        )
