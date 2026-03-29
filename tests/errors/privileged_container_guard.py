"""
Test for privileged container guard — hard block on security.privileged=true.

Tests that:
1. Setting security.privileged=true on the default profile causes coi run to fail
2. The error message is actionable
"""

import subprocess


def test_privileged_default_profile_blocked(coi_binary):
    """
    Verify coi run fails when the default Incus profile has security.privileged=true.

    Flow:
    1. Set security.privileged=true on the default profile
    2. Run coi run echo hello
    3. Verify it fails with an actionable security error
    4. Always restore the profile in the finally block
    """
    try:
        # Set privileged on the default profile
        setup = subprocess.run(
            ["incus", "profile", "set", "default", "security.privileged=true"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert setup.returncode == 0, (
            f"Failed to set security.privileged=true on default profile: {setup.stderr}"
        )

        # Run coi run — should fail
        result = subprocess.run(
            [coi_binary, "run", "echo", "hello"],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode != 0, (
            f"coi run should fail with privileged default profile. "
            f"Exit code: {result.returncode}\nstdout: {result.stdout}\nstderr: {result.stderr}"
        )

        error_output = result.stderr
        assert "security.privileged" in error_output or "privileged" in error_output.lower(), (
            f"Error should mention security.privileged. Got:\n{error_output}"
        )
        assert "incus profile unset" in error_output or "unprivileged" in error_output.lower(), (
            f"Error should be actionable. Got:\n{error_output}"
        )

    finally:
        # Always restore the default profile
        subprocess.run(
            ["incus", "profile", "unset", "default", "security.privileged"],
            capture_output=True,
            timeout=10,
            check=False,
        )
