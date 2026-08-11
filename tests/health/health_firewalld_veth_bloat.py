"""
The firewalld_veth_bloat health check (#695): dead veth interfaces registered
in firewalld's zones make firewalld generate FORWARD rules quadratically
(cross product of zone interfaces) — 145 leaked veths ≈ 101k rules on the
reporting host. The check must warn when `table inet firewalld` references
veths that no longer exist, and stay healthy when firewalld is absent.

The test synthesizes the firewalld table with rules referencing nonexistent
veths — exactly the state NetworkManager leaves behind — and asserts the
check's warning end-to-end in JSON output, then removes the table and asserts
the healthy path. Skipped when a REAL firewalld table already exists (never
touch a live firewall) or when nft/sudo aren't usable.
"""

import json
import subprocess


def nft(*args):
    return subprocess.run(
        ["sudo", "-n", "nft", *args],
        capture_output=True,
        text=True,
        timeout=30,
    )


def run_health(coi_binary, workspace_dir):
    result = subprocess.run(
        [coi_binary, "health", "--format", "json", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )
    assert result.returncode in (0, 1, 2), f"health should run. stderr: {result.stderr}"
    return json.loads(result.stdout)["checks"]["firewalld_veth_bloat"]


def test_health_firewalld_veth_bloat(coi_binary, workspace_dir):
    import pytest

    probe = nft("list", "tables")
    if probe.returncode != 0:
        pytest.skip(f"nft not usable via sudo: {probe.stderr}")
    if "inet firewalld" in probe.stdout:
        pytest.skip("real firewalld table present; not touching a live firewall")

    try:
        # Synthesize the leak: a firewalld table whose rules reference veths
        # that don't exist (NetworkManager's leftover zone registrations).
        assert nft("add", "table", "inet", "firewalld").returncode == 0
        assert nft("add", "chain", "inet", "firewalld", "filter_FORWARD").returncode == 0
        for i in range(4):
            r = nft(
                "add",
                "rule",
                "inet",
                "firewalld",
                "filter_FORWARD",
                "iifname",
                f"vethdead000{i}",
                "oifname",
                "vethdead0009",
                "drop",
            )
            assert r.returncode == 0, r.stderr

        check = run_health(coi_binary, workspace_dir)
        assert check["status"] == "warning", f"expected warning, got: {check}"
        assert check["details"]["dead_veths"] == 5, f"details: {check['details']}"
        assert "firewall-cmd --reload" in check["message"], check["message"]
        assert "unmanaged-devices" in check["message"], check["message"]
    finally:
        nft("delete", "table", "inet", "firewalld")

    # Healthy path: table gone -> OK, no scary message.
    check = run_health(coi_binary, workspace_dir)
    assert check["status"] == "ok", f"expected ok without firewalld, got: {check}"
