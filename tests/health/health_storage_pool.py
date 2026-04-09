"""
Test for coi health - Incus storage pools check.

Verifies that:
1. The incus_storage_pools check is present in health output
2. It reports per-pool used/free/total GiB in its details
3. Status is ok or warning (not failed) on a healthy system
4. Text output contains "Incus storage pools" label in STORAGE section
"""

import json
import subprocess


def test_health_storage_pools_present_in_json(coi_binary):
    """
    Verify incus_storage_pools check is present with per-pool structure in JSON output.
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

    assert "incus_storage_pools" in checks, (
        "incus_storage_pools check should be present in health output"
    )

    pool_check = checks["incus_storage_pools"]

    # Status must be ok or warning on a working system (not failed)
    assert pool_check["status"] in ["ok", "warning"], (
        f"incus_storage_pools status should be ok or warning, got: {pool_check['status']}"
    )

    # Message should mention GiB usage for at least one pool
    assert "GiB" in pool_check["message"] or "G" in pool_check["message"], (
        f"incus_storage_pools message should contain GiB usage, got: {pool_check['message']}"
    )

    # Details is a per-pool nested map
    details = pool_check.get("details", {})
    assert isinstance(details, dict), "details should be a dict"
    assert len(details) > 0, "details should contain at least one pool"

    for pool_name, entry in details.items():
        assert isinstance(entry, dict), (
            f"details[{pool_name!r}] should be a dict, got {type(entry).__name__}"
        )
        # Skip pools that reported failed (e.g. in test environments)
        if entry.get("status") == "failed":
            continue
        assert "used_gib" in entry, f"pool {pool_name!r} entry should contain 'used_gib'"
        assert "total_gib" in entry, f"pool {pool_name!r} entry should contain 'total_gib'"
        assert "free_gib" in entry, f"pool {pool_name!r} entry should contain 'free_gib'"
        assert "used_pct" in entry, f"pool {pool_name!r} entry should contain 'used_pct'"
        assert entry["total_gib"] > 0, f"pool {pool_name!r} total_gib should be positive"
        assert entry["free_gib"] >= 0, f"pool {pool_name!r} free_gib should be non-negative"
        assert 0 <= entry["used_pct"] <= 100, (
            f"pool {pool_name!r} used_pct should be 0-100, got: {entry['used_pct']}"
        )


def test_health_storage_pools_in_text_output(coi_binary):
    """
    Verify incus_storage_pools appears in the STORAGE section of text output.
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
    assert "Incus storage pools" in output, (
        "STORAGE section should contain 'Incus storage pools' check"
    )
