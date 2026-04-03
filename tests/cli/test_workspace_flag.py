"""
Test that the attach command uses the global --workspace flag
instead of a shadowed local definition.

Bug #11: attach previously defined its own --workspace/-w flag,
which silently shadowed the global one from rootCmd.
"""

import subprocess


def test_attach_uses_global_workspace_flag(coi_binary, workspace_dir):
    """coi attach --workspace <dir> --slot 1 should not produce a flag conflict error.

    The container won't exist, so we expect "not found or not running" — but
    critically NOT a cobra flag-registration error like
    "flag redefined: workspace".
    """
    result = subprocess.run(
        [coi_binary, "attach", "--workspace", workspace_dir, "--slot", "1"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    combined = result.stdout + result.stderr

    # Must NOT see flag conflict / redefinition errors
    assert "flag redefined" not in combined.lower(), f"Flag shadowing detected: {combined}"
    assert "unknown flag" not in combined.lower(), f"Unexpected unknown flag error: {combined}"

    # We expect a non-zero exit (container not found), but the error should
    # be about the container, not about flags.
    assert result.returncode != 0, "Should fail because container doesn't exist"
    assert "not found" in combined.lower() or "not running" in combined.lower(), (
        f"Expected container-not-found error, got: {combined}"
    )
