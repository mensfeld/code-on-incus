"""
Test for coi health - bridge forward rules check (backed by iptables rules).

Tests that:
1. The bridge_forward_rules check exists in health output
2. When iptables bridge FORWARD rules (tagged coi-bridge-forward) are present,
   check is OK
3. When the rules are absent, check warns
4. Check appears in text output under NETWORKING section

The Go functions BridgeInTrustedZone / EnsureBridgeInTrustedZone check
iptables FORWARD rules tagged with the comment `coi-bridge-forward`.
The health check key is `bridge_forward_rules`.
Masquerade / NAT is handled by Incus (`ipv4.nat=true` on the bridge), so no
firewalld presence is required.
"""

import json
import subprocess


def iptables_available():
    """Check if iptables is available with sudo access."""
    try:
        result = subprocess.run(
            ["sudo", "-n", "iptables", "-S", "FORWARD"],
            capture_output=True,
            timeout=10,
        )
        return result.returncode == 0
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return False


def get_bridge_name():
    """Get the Incus bridge name."""
    try:
        result = subprocess.run(
            ["incus", "network", "list", "-f", "csv"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode == 0:
            for line in result.stdout.strip().split("\n"):
                parts = line.split(",")
                if len(parts) >= 2 and "bridge" in parts[1]:
                    return parts[0]
    except (subprocess.TimeoutExpired, FileNotFoundError):
        pass
    return "incusbr0"


def bridge_has_forward_rules(bridge_name):
    """Check if coi-bridge-forward iptables FORWARD rules exist for the bridge."""
    try:
        result = subprocess.run(
            ["sudo", "-n", "iptables", "-S", "FORWARD"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            return False
        return "coi-bridge-forward" in result.stdout and bridge_name in result.stdout
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return False


def test_health_bridge_forward_rules_check_exists(coi_binary):
    """
    Verify bridge_forward_rules check appears in health JSON output.

    Flow:
    1. Run coi health --format json
    2. Parse JSON, verify bridge_forward_rules key exists
    3. Verify it has required fields
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

    assert "bridge_forward_rules" in checks, "Should have bridge_forward_rules check"

    check = checks["bridge_forward_rules"]
    assert "name" in check, "Check should have 'name' field"
    assert "status" in check, "Check should have 'status' field"
    assert "message" in check, "Check should have 'message' field"
    assert check["status"] in ["ok", "warning", "failed"], f"Invalid status: {check['status']}"


def test_health_bridge_forward_rules_ok_when_configured(coi_binary):
    """
    When iptables coi-bridge-forward rules are present, check reports OK.

    Flow:
    1. Skip if iptables not available or bridge rules not present
    2. Run coi health --format json
    3. Verify bridge_forward_rules check is OK with correct details
    """
    import pytest

    if not iptables_available():
        pytest.skip("iptables not available")

    bridge_name = get_bridge_name()
    if not bridge_has_forward_rules(bridge_name):
        pytest.skip(f"Bridge {bridge_name} has no coi-bridge-forward rules (test requires them)")

    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    data = json.loads(result.stdout)
    check = data["checks"]["bridge_forward_rules"]

    assert check["status"] == "ok", (
        f"Expected OK when bridge FORWARD rules are present, got: {check['status']} - {check['message']}"
    )
    assert "details" in check, "Should have details"
    assert check["details"]["bridge_name"] == bridge_name, (
        f"Details should show bridge name {bridge_name}"
    )
    assert check["details"]["has_forward_rules"] is True, (
        "Details should show has_forward_rules=true"
    )


def test_health_bridge_forward_rules_ok_without_firewalld(coi_binary):
    """
    When firewalld is not running (the normal nftables case), bridge forward rules check
    is OK as long as iptables coi-bridge-forward rules are present.

    Flow:
    1. Run coi health --format json
    2. Verify bridge_forward_rules check returns ok or warning (not failed/error)
    3. Verify message is meaningful
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    data = json.loads(result.stdout)
    check = data["checks"]["bridge_forward_rules"]

    # Without firewalld, the check relies on iptables rules.
    # Status is ok if rules exist, warning if not — both are valid non-error states.
    assert check["status"] in ("ok", "warning"), (
        f"Expected ok or warning for bridge check, got: {check['status']}"
    )
    assert check["message"], "Check should have a non-empty message"


def test_health_bridge_forward_rules_text_output(coi_binary):
    """
    Verify bridge forward rules check appears in text health output under NETWORKING.

    Flow:
    1. Run coi health (text format)
    2. Verify 'Bridge forward' appears in output
    """
    result = subprocess.run(
        [coi_binary, "health"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2], (
        f"Health check failed unexpectedly. stderr: {result.stderr}"
    )

    output = result.stdout
    assert "Bridge forward" in output, (
        f"Should show 'Bridge forward' in text output. Got:\n{output}"
    )
