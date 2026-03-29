"""
Test for coi health — privileged profile check appears and detects misconfiguration.

Tests that:
1. Privileged check appears in text and JSON output
2. When default profile is unprivileged, status is "ok"
3. When default profile has security.privileged=true, status is "failed"
"""

import json
import subprocess


def test_health_privileged_profile_ok(coi_binary):
    """
    Verify coi health shows privileged profile check as OK when profile is safe.
    """
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

    assert result.returncode in [0, 1], (
        f"Health check failed with exit {result.returncode}. stderr: {result.stderr}"
    )

    output = result.stdout
    assert "Privileged check" in output, (
        f"Should show Privileged check in text output. Got:\n{output}"
    )


def test_health_privileged_profile_detects_misconfiguration(coi_binary):
    """
    Verify coi health detects security.privileged=true on the default profile.

    Flow:
    1. Set security.privileged=true on default profile
    2. Run coi health --format json
    3. Verify privileged_profile check is "failed"
    4. Always restore profile in finally block
    """
    try:
        # Set privileged on default profile
        setup = subprocess.run(
            ["incus", "profile", "set", "default", "security.privileged=true"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert setup.returncode == 0, (
            f"Failed to set security.privileged=true: {setup.stderr}"
        )

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
        # Always restore the default profile
        subprocess.run(
            ["incus", "profile", "unset", "default", "security.privileged"],
            capture_output=True,
            timeout=10,
            check=False,
        )
