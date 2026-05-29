"""
Tests for iptables fallback when Docker sets FORWARD policy to DROP and
firewalld is not available.

Tests:
1. `coi health --format json` includes the docker_forward_policy check
2. On a Docker-with-no-firewalld system, `coi build` succeeds using the fallback
"""

import json
import subprocess


def nft_available():
    """Check if nft is available with sudo access."""
    try:
        result = subprocess.run(
            ["sudo", "-n", "nft", "list", "tables"],
            capture_output=True,
            timeout=10,
        )
        return result.returncode == 0
    except Exception:
        return False


def docker_running():
    """Check if Docker is running."""
    try:
        result = subprocess.run(
            ["systemctl", "is-active", "--quiet", "docker"],
            capture_output=True,
            timeout=10,
        )
        return result.returncode == 0
    except Exception:
        return False


def forward_policy_is_drop():
    """Check if iptables FORWARD chain policy is DROP."""
    try:
        result = subprocess.run(
            ["sudo", "-n", "iptables", "-L", "FORWARD", "-n"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            return False
        first_line = result.stdout.split("\n")[0] if result.stdout else ""
        return "policy DROP" in first_line
    except Exception:
        return False


def iptables_bridge_rules_exist(bridge_name):
    """Check if coi-bridge-forward iptables rules exist."""
    try:
        result = subprocess.run(
            ["sudo", "-n", "iptables", "-S", "FORWARD"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            return False
        return "coi-bridge-forward" in result.stdout
    except Exception:
        return False


def test_health_reports_docker_forward_policy(coi_binary):
    """Verify `coi health --format json` includes the docker_forward_policy check
    with expected detail fields."""
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # coi health exits 0 (healthy), 1 (degraded), or 2 (unhealthy) — all are valid
    assert result.returncode in (0, 1, 2), (
        f"Unexpected exit code {result.returncode}: {result.stderr}"
    )

    data = json.loads(result.stdout)
    checks = data.get("checks", {})

    assert "docker_forward_policy" in checks, (
        f"docker_forward_policy check missing from health output. Keys: {list(checks.keys())}"
    )

    check = checks["docker_forward_policy"]
    assert check["status"] in ("ok", "warning", "failed")
    assert check["name"] == "docker_forward_policy"
    assert "message" in check

    details = check.get("details", {})
    expected_fields = [
        "docker_running",
        "forward_policy_drop",
        "nft_available",
        "iptables_available",
    ]
    for field in expected_fields:
        assert field in details, (
            f"Missing detail field '{field}' in docker_forward_policy check. Details: {details}"
        )


def test_iptables_fallback_during_build(coi_binary):
    """On a system with Docker FORWARD DROP and no firewalld, verify coi build
    succeeds using the iptables fallback.

    Skipped when:
    - firewalld IS available (fallback not needed)
    - Docker not running or FORWARD not DROP (scenario doesn't apply)
    """
    import pytest

    if not nft_available():
        pytest.skip("nft not available — cannot test fallback")
    if not docker_running():
        pytest.skip("Docker not running — scenario does not apply")
    if not forward_policy_is_drop():
        pytest.skip("FORWARD policy is not DROP — scenario does not apply")

    # Run coi build (force rebuild to exercise the full path)
    result = subprocess.run(
        [coi_binary, "build", "--force"],
        capture_output=True,
        text=True,
        timeout=600,  # builds can take a while
    )

    assert result.returncode == 0, (
        f"coi build failed (exit {result.returncode}):\n"
        f"stdout: {result.stdout[-2000:]}\n"
        f"stderr: {result.stderr[-2000:]}"
    )

    # After build completes, iptables rules should have been cleaned up
    assert not iptables_bridge_rules_exist("incusbr0"), (
        "iptables coi-bridge-forward rules were not cleaned up after build"
    )
