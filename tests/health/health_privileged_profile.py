"""
Test for coi health — privileged profile check appears and detects misconfiguration.

Tests that:
1. Privileged check appears in text and JSON output
2. When default profile is unprivileged, status is "ok"
3. When default profile has security.privileged=true, status is "failed"
"""

import json
import subprocess


def _get_privileged_value():
    """Get the current security.privileged value from the default profile."""
    result = subprocess.run(
        ["incus", "profile", "get", "default", "security.privileged"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode == 0:
        return result.stdout.strip() or None
    return None


def _restore_privileged_value(original_value):
    """Restore the original security.privileged value on the default profile."""
    if original_value is None or original_value == "":
        subprocess.run(
            ["incus", "profile", "unset", "default", "security.privileged"],
            capture_output=True,
            timeout=10,
            check=False,
        )
    else:
        subprocess.run(
            ["incus", "profile", "set", "default", f"security.privileged={original_value}"],
            capture_output=True,
            timeout=10,
            check=False,
        )


def test_health_privileged_profile_ok(coi_binary):
    """
    Verify coi health shows privileged profile check as OK when profile is unprivileged.

    Saves and restores the original profile value to be self-contained.
    """
    original_value = _get_privileged_value()
    try:
        # Ensure unprivileged state
        if original_value == "true":
            subprocess.run(
                ["incus", "profile", "unset", "default", "security.privileged"],
                capture_output=True,
                timeout=10,
                check=False,
            )

        result = subprocess.run(
            [coi_binary, "health", "--format", "json"],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode in [0, 1], (
            f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
        )

        data = json.loads(result.stdout)
        checks = data["checks"]

        assert "privileged_profile" in checks, "Should have privileged_profile check"

        pp = checks["privileged_profile"]
        assert pp["status"] == "ok", (
            f"Default profile should be unprivileged. Got: {pp['status']} — {pp['message']}"
        )
    finally:
        _restore_privileged_value(original_value)


def test_health_privileged_profile_text(coi_binary):
    """
    Verify coi health text output includes the privileged check.
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
    assert "Privileged check" in output, (
        f"Should show Privileged check in text output. Got:\n{output}"
    )


def test_health_privileged_profile_detects_misconfiguration(coi_binary):
    """
    Verify coi health detects security.privileged=true on the default profile.

    Saves and restores the original profile value to be self-contained.
    """
    original_value = _get_privileged_value()
    try:
        # Set privileged on default profile
        setup = subprocess.run(
            ["incus", "profile", "set", "default", "security.privileged=true"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert setup.returncode == 0, f"Failed to set security.privileged=true: {setup.stderr}"

        # Run health check
        result = subprocess.run(
            [coi_binary, "health", "--format", "json"],
            capture_output=True,
            text=True,
            timeout=120,
        )

        # Health should report unhealthy (exit 2) since this is a failed check
        assert result.returncode == 2, (
            f"Health should be unhealthy with privileged profile. "
            f"Exit code: {result.returncode}\nstdout: {result.stdout}"
        )

        data = json.loads(result.stdout)
        checks = data["checks"]

        assert "privileged_profile" in checks, "Should have privileged_profile check"

        pp = checks["privileged_profile"]
        assert pp["status"] == "failed", (
            f"privileged_profile should be 'failed' when profile is privileged. "
            f"Got: {pp['status']} — {pp['message']}"
        )
        assert "security.privileged" in pp["message"], (
            f"Message should mention security.privileged. Got: {pp['message']}"
        )

    finally:
        _restore_privileged_value(original_value)
