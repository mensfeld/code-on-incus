"""
Test for coi health - Incus storage pool check.

Verifies that:
1. The incus_storage_pool check is present in health output
2. It reports pool name, used/free/total GiB in its details
3. Status is ok or warning (not failed) on a healthy system
4. Text output contains "Incus storage pool" label in STORAGE section
"""

import json
import subprocess


def test_health_storage_pool_present_in_json(coi_binary):
    """
    Verify incus_storage_pool check is present with correct structure in JSON output.
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode in [0, 1], (
        f"Health check exited {result.returncode}. stderr: {result.stderr}"
    )

    data = json.loads(result.stdout)
    checks = data["checks"]

    assert "incus_storage_pool" in checks, (
        "incus_storage_pool check should be present in health output"
    )

    pool_check = checks["incus_storage_pool"]

    # Status must be ok or warning on a working system (not failed)
    assert pool_check["status"] in ["ok", "warning"], (
        f"incus_storage_pool status should be ok or warning, got: {pool_check['status']}"
    )

    # Message should mention the pool name and GiB usage
    assert "GiB" in pool_check["message"], (
        f"incus_storage_pool message should contain GiB usage, got: {pool_check['message']}"
    )

    # Details should contain pool, used_gib, total_gib, free_gib, used_pct
    details = pool_check.get("details", {})
    assert "pool" in details, "incus_storage_pool details should contain 'pool' name"
    assert "used_gib" in details, "incus_storage_pool details should contain 'used_gib'"
    assert "total_gib" in details, "incus_storage_pool details should contain 'total_gib'"
    assert "free_gib" in details, "incus_storage_pool details should contain 'free_gib'"
    assert "used_pct" in details, "incus_storage_pool details should contain 'used_pct'"

    # Sanity: total > 0, free >= 0, used_pct in [0, 100]
    assert details["total_gib"] > 0, "total_gib should be positive"
    assert details["free_gib"] >= 0, "free_gib should be non-negative"
    assert 0 <= details["used_pct"] <= 100, f"used_pct should be 0-100, got: {details['used_pct']}"


def test_health_storage_pool_in_text_output(coi_binary):
    """
    Verify incus_storage_pool appears in the STORAGE section of text output.
    """
    result = subprocess.run(
        [coi_binary, "health"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode in [0, 1], (
        f"Health check exited {result.returncode}. stderr: {result.stderr}"
    )

    output = result.stdout

    assert "STORAGE:" in output, "Should have STORAGE section"
    assert "Incus storage pool" in output, (
        "STORAGE section should contain 'Incus storage pool' check"
    )

    # The message should include GiB and a pool name
    assert "GiB" in output, "Storage pool output should mention GiB"
