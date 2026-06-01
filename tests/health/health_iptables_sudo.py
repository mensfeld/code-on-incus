"""
Tests for coi health - iptables sudo check.

Verifies that:
1. The 'iptables_sudo' check is present in health JSON output
2. Its status is always 'ok' or 'warning', never 'failed'
3. The text output shows the check under NETWORKING
4. When the status is 'warning', the message contains a sudoers.d fix command
5. When the status is 'ok', the message is meaningful
"""

import json
import subprocess


def test_iptables_sudo_check_present(coi_binary):
    """
    The 'iptables_sudo' check must appear in coi health --format json.
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2], (
        f"health exited with unexpected code {result.returncode}. stderr: {result.stderr}"
    )

    data = json.loads(result.stdout)
    checks = data["checks"]

    assert "iptables_sudo" in checks, (
        f"Expected 'iptables_sudo' check in health output. Got keys: {sorted(checks.keys())}"
    )


def test_iptables_sudo_check_valid_status(coi_binary):
    """
    The 'iptables_sudo' check must be 'ok' or 'warning' — never 'failed'.
    The check probes a read-only iptables command; if sudo isn't configured
    it degrades to a warning with a fix, it never hard-fails.
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2]
    check = json.loads(result.stdout)["checks"]["iptables_sudo"]

    assert check["status"] in ("ok", "warning"), (
        f"iptables_sudo status must be 'ok' or 'warning', got: {check['status']!r} "
        f"(message: {check['message']!r})"
    )


def test_iptables_sudo_check_required_fields(coi_binary):
    """
    The 'iptables_sudo' check must have name, status, and message fields.
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2]
    check = json.loads(result.stdout)["checks"]["iptables_sudo"]

    assert check.get("name") == "iptables_sudo", (
        f"Check name field should be 'iptables_sudo', got: {check.get('name')!r}"
    )
    assert "status" in check, "Check must have 'status' field"
    assert "message" in check, "Check must have 'message' field"
    assert check["message"], "Message must not be empty"


def test_iptables_sudo_warning_includes_fix_command(coi_binary):
    """
    When iptables sudo is not configured the warning message must include
    the sudoers.d fix command so the user knows how to resolve it.
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2]
    check = json.loads(result.stdout)["checks"]["iptables_sudo"]

    if check["status"] == "warning":
        assert "sudoers.d" in check["message"], (
            f"Warning message should include a sudoers.d fix path. Got: {check['message']!r}"
        )
        assert "iptables" in check["message"].lower(), (
            f"Warning message should reference iptables. Got: {check['message']!r}"
        )


def test_iptables_sudo_check_in_networking_section(coi_binary):
    """
    The 'iptables sudo' label must appear in the NETWORKING section of the
    text health output, positioned between 'Bridge forward' and 'Docker FORWARD'.
    """
    result = subprocess.run(
        [coi_binary, "health"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2]
    output = result.stdout

    assert "iptables sudo" in output, (
        f"Expected 'iptables sudo' label in health text output. Got:\n{output}"
    )

    # Verify it sits inside the NETWORKING section (before STORAGE:)
    networking_start = output.find("NETWORKING:")
    storage_start = output.find("STORAGE:")
    iptables_pos = output.find("iptables sudo")

    assert networking_start != -1, "NETWORKING section must exist"
    assert iptables_pos != -1, "iptables sudo label must exist"
    if storage_start != -1:
        assert networking_start < iptables_pos < storage_start, (
            "iptables sudo must appear inside NETWORKING section (between NETWORKING: and STORAGE:)"
        )


def test_iptables_sudo_ok_message_meaningful(coi_binary):
    """
    When iptables sudo is configured, the OK message should confirm it clearly.
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2]
    check = json.loads(result.stdout)["checks"]["iptables_sudo"]

    if check["status"] == "ok":
        msg = check["message"].lower()
        # The message should say something meaningful — not be empty or generic
        assert any(word in msg for word in ("configured", "not required", "macos")), (
            f"OK message should be descriptive, got: {check['message']!r}"
        )
