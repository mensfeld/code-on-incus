"""
Test that `coi health` reports a multi-pool storage check by default.
"""

import json
import subprocess


def test_health_storage_pools_default(coi_binary):
    """JSON output should include incus_storage_pools check with details."""
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode in (0, 1), f"health should return 0 or 1. stderr: {result.stderr}"

    data = json.loads(result.stdout)
    checks = data.get("checks", {})
    assert "incus_storage_pools" in checks, (
        f"Should include incus_storage_pools check. Got: {list(checks.keys())}"
    )

    pool_check = checks["incus_storage_pools"]
    assert pool_check["status"] in ("ok", "warning", "failed"), (
        f"Status should be one of ok/warning/failed. Got: {pool_check['status']}"
    )

    details = pool_check.get("details", {})
    assert isinstance(details, dict), f"Details should be a dict. Got: {type(details)}"
    assert len(details) >= 1, f"Should have at least one pool entry. Got: {details}"

    # Each entry must be a per-pool dict
    for pool_name, entry in details.items():
        assert isinstance(entry, dict), (
            f"Pool entry {pool_name!r} should be a dict. Got: {type(entry)}"
        )
        # If the pool was reachable, check the shape; otherwise it's a failed entry
        if entry.get("status") == "failed":
            continue
        assert "used_gib" in entry, f"Missing used_gib in {pool_name}: {entry}"
        assert "total_gib" in entry, f"Missing total_gib in {pool_name}: {entry}"
        assert "free_gib" in entry, f"Missing free_gib in {pool_name}: {entry}"
        assert "used_pct" in entry, f"Missing used_pct in {pool_name}: {entry}"
