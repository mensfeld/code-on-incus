"""
Test for coi health - nft firewall check (formerly "firewall" / CheckFirewall).

Tests that:
1. The "nft" check exists in health JSON output (renamed from "firewall")
2. Check has nft_installed and nft_available detail fields
3. Text output shows "nft firewall" label
4. Orphan health check uses "orphaned_nft_rules" key (not "orphaned_firewall_rules")
"""

import json
import subprocess


def test_health_nft_check_exists(coi_binary):
    """
    Verify the "nft" check (formerly "firewall") appears in health JSON.

    Flow:
    1. Run coi health --format json
    2. Verify "nft" key exists in checks (not the old "firewall" key)
    3. Verify required fields are present
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2], (
        f"Health check failed unexpectedly. stderr: {result.stderr}"
    )

    data = json.loads(result.stdout)
    checks = data["checks"]

    assert "nft" in checks, (
        f"Should have 'nft' check (renamed from 'firewall'). Got keys: {list(checks.keys())}"
    )
    assert "firewall" not in checks, (
        "Old 'firewall' key should no longer exist — it was renamed to 'nft'"
    )

    check = checks["nft"]
    assert "name" in check and check["name"] == "nft", (
        f"Check name should be 'nft', got: {check.get('name')}"
    )
    assert "status" in check, "nft check should have 'status'"
    assert "message" in check, "nft check should have 'message'"
    assert check["status"] in ("ok", "warning", "failed"), f"Invalid status: {check['status']}"


def test_health_nft_check_detail_fields(coi_binary):
    """
    Verify the "nft" check includes nft_installed and nft_available detail fields.

    Flow:
    1. Run coi health --format json
    2. Check that nft check has details with nft_installed and nft_available
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2]
    data = json.loads(result.stdout)
    check = data["checks"].get("nft")
    assert check is not None, "nft check should exist"

    details = check.get("details", {})
    assert "nft_installed" in details, (
        f"nft check should have 'nft_installed' detail. Got: {list(details.keys())}"
    )
    assert "nft_available" in details, (
        f"nft check should have 'nft_available' detail. Got: {list(details.keys())}"
    )
    assert isinstance(details["nft_installed"], bool), "nft_installed should be bool"
    assert isinstance(details["nft_available"], bool), "nft_available should be bool"


def test_health_nft_text_label(coi_binary):
    """
    Verify "nft firewall" appears in text health output under NETWORKING.

    Flow:
    1. Run coi health (text output)
    2. Verify "nft firewall" label appears (the renamed display label)
    """
    result = subprocess.run(
        [coi_binary, "health"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2]
    assert "nft firewall" in result.stdout, (
        f"Expected 'nft firewall' label in text output. Got:\n{result.stdout}"
    )


def test_health_orphan_check_uses_nft_key(coi_binary):
    """
    Verify the orphan health check uses "orphaned_nft_rules" detail key
    (renamed from "orphaned_firewall_rules").

    Flow:
    1. Run coi health --format json --verbose
    2. If orphan check is present and has details, verify key name
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json", "--verbose"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2]
    data = json.loads(result.stdout)
    checks = data["checks"]

    orphan_check = checks.get("orphaned_resources")
    if orphan_check is None:
        return  # check may not appear in all configs

    details = orphan_check.get("details", {})
    if details:
        assert "orphaned_nft_rules" in details, (
            f"Orphan check should use 'orphaned_nft_rules' key (not 'orphaned_firewall_rules'). "
            f"Got: {list(details.keys())}"
        )
        assert "orphaned_firewall_rules" not in details, (
            "Old 'orphaned_firewall_rules' key should no longer exist"
        )
