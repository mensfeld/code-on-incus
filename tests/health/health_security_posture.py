"""
Test for coi health — security posture check.

Tests that:
1. Security posture appears in text output
2. security_posture appears in JSON output with correct structure and details
3. On a clean default profile, status is "ok"
4. When security.privileged=true, status is "failed"
5. When raw.seccomp is overridden, status is "warning"
6. When raw.apparmor is overridden, status is "warning"
"""

import json
import subprocess

import pytest


def _get_profile_value(key):
    """Get a config value from the default profile."""
    result = subprocess.run(
        ["incus", "profile", "get", "default", key],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode == 0:
        return result.stdout.strip() or None
    return None


def _set_profile_value(key, value):
    """Set a config value on the default profile."""
    result = subprocess.run(
        ["incus", "profile", "set", "default", f"{key}={value}"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, f"Failed to set {key}={value}: {result.stderr}"


def _unset_profile_value(key):
    """Unset a config value on the default profile."""
    subprocess.run(
        ["incus", "profile", "unset", "default", key],
        capture_output=True,
        timeout=10,
        check=False,
    )


def _restore_profile_value(key, original_value):
    """Restore a profile config value to its original state."""
    if original_value is None or original_value == "":
        _unset_profile_value(key)
    else:
        _set_profile_value(key, original_value)


def _run_health_json(coi_binary):
    """Run coi health --format json and return parsed security_posture check."""
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )
    data = json.loads(result.stdout)
    return result.returncode, data["checks"]["security_posture"]


_RAW_OVERRIDE_KEYS = ("raw.seccomp", "raw.apparmor")


@pytest.fixture(scope="module", autouse=True)
def _snapshot_default_raw_overrides():
    """Capture the real pre-suite state of the shared default profile's raw
    overrides once, and restore it verbatim when the module is done.

    The warning tests below deliberately mutate raw.seccomp/raw.apparmor on the
    *shared* default profile. This snapshot ensures that whatever a developer (or
    the environment) had configured before the suite ran is put back afterwards,
    so the file leaves no lasting footprint.
    """
    saved = {key: _get_profile_value(key) for key in _RAW_OVERRIDE_KEYS}
    yield
    for key, value in saved.items():
        _restore_profile_value(key, value)


@pytest.fixture(autouse=True)
def _clean_default_raw_overrides(_snapshot_default_raw_overrides):
    """Guarantee every test in this file starts and ends with no raw override on
    the shared default profile.

    pytest-randomly shuffles test order and pytest-rerunfailures can re-enter a
    test, so a sibling's raw.seccomp/raw.apparmor override could otherwise bleed
    into test_health_security_posture_ok_on_default and flake it. Worse, each
    warning test snapshots its own "original" value and restores it in a finally;
    once the profile is polluted, that restore perpetuates the override forever.
    Force-clearing before and after each test breaks that cycle without touching
    the developer's real config (restored by the module fixture above).
    """
    for key in _RAW_OVERRIDE_KEYS:
        _unset_profile_value(key)
    yield
    for key in _RAW_OVERRIDE_KEYS:
        _unset_profile_value(key)


def test_health_security_posture_text(coi_binary):
    """
    Verify coi health text output includes the Security posture check.
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
    assert "Security posture" in output, (
        f"Should show Security posture in text output. Got:\n{output}"
    )


def test_health_security_posture_json(coi_binary):
    """
    Verify coi health JSON output includes security_posture with correct structure.
    """
    result = subprocess.run(
        [coi_binary, "health", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode in [0, 1, 2], (
        f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
    )

    data = json.loads(result.stdout)
    checks = data["checks"]

    assert "security_posture" in checks, "Should have security_posture check"

    sp = checks["security_posture"]
    assert sp["name"] == "security_posture", (
        f"Check name should be 'security_posture'. Got: {sp['name']}"
    )
    assert sp["status"] in ["ok", "warning", "failed"], f"Invalid status: {sp['status']}"
    assert "message" in sp, "Should have a message"

    # Verify details are present (unless skipped)
    if "Skipped" not in sp["message"]:
        assert "details" in sp, "Should have details when not skipped"
        details = sp["details"]
        assert "seccomp" in details, "Details should include seccomp"
        assert "apparmor" in details, "Details should include apparmor"
        assert "privileged" in details, "Details should include privileged"
        assert "raw_seccomp_override" in details, "Details should include raw_seccomp_override"
        assert "raw_apparmor_override" in details, "Details should include raw_apparmor_override"


def test_health_security_posture_ok_on_default(coi_binary):
    """
    On a clean default profile (unprivileged, no raw overrides), status should be "ok".
    """
    rc, sp = _run_health_json(coi_binary)

    # The aggregate health exit code reflects ALL checks (network/storage/incus/…),
    # any of which can transiently fail under CI load and push the overall exit to
    # 2 even when the security posture is fine. This test is specifically about the
    # security_posture check, so assert on that status rather than the aggregate rc.
    assert sp["status"] == "ok", (
        f"Default profile should have ok security posture (overall health exit {rc}). "
        f"Got: {sp['status']} — {sp['message']}"
    )


def test_health_security_posture_failed_when_privileged(coi_binary):
    """
    Verify security_posture is "failed" when security.privileged=true.

    Saves and restores the original profile value to be self-contained.
    """
    original = _get_profile_value("security.privileged")
    try:
        _set_profile_value("security.privileged", "true")

        rc, sp = _run_health_json(coi_binary)

        assert rc == 2, f"Health should be unhealthy with privileged profile. Exit code: {rc}"
        assert sp["status"] == "failed", (
            f"security_posture should be 'failed' when privileged. "
            f"Got: {sp['status']} — {sp['message']}"
        )
        assert sp["details"]["privileged"] is True, "Details should show privileged=true"
        assert "disabled" in sp["details"]["seccomp"], "Seccomp should be disabled"
        assert "disabled" in sp["details"]["apparmor"], "AppArmor should be disabled"
    finally:
        _restore_profile_value("security.privileged", original)


def test_health_security_posture_warning_on_raw_seccomp(coi_binary):
    """
    Verify security_posture is "warning" when raw.seccomp is overridden.

    Saves and restores the original profile value to be self-contained.
    """
    original = _get_profile_value("raw.seccomp")
    try:
        _set_profile_value("raw.seccomp", "2 1 amd64")

        rc, sp = _run_health_json(coi_binary)

        assert rc in [1, 2], f"Health should be degraded or unhealthy. Exit code: {rc}"
        assert sp["status"] == "warning", (
            f"security_posture should be 'warning' with raw.seccomp override. "
            f"Got: {sp['status']} — {sp['message']}"
        )
        assert sp["details"]["raw_seccomp_override"] is True, (
            "Details should show raw_seccomp_override=true"
        )
    finally:
        _restore_profile_value("raw.seccomp", original)


def test_health_security_posture_warning_on_raw_apparmor(coi_binary):
    """
    Verify security_posture is "warning" when raw.apparmor is overridden.

    Saves and restores the original profile value to be self-contained.
    """
    original = _get_profile_value("raw.apparmor")
    try:
        _set_profile_value("raw.apparmor", "/usr/bin/** rix,")

        rc, sp = _run_health_json(coi_binary)

        assert rc in [1, 2], f"Health should be degraded or unhealthy. Exit code: {rc}"
        assert sp["status"] == "warning", (
            f"security_posture should be 'warning' with raw.apparmor override. "
            f"Got: {sp['status']} — {sp['message']}"
        )
        assert sp["details"]["raw_apparmor_override"] is True, (
            "Details should show raw_apparmor_override=true"
        )
    finally:
        _restore_profile_value("raw.apparmor", original)
