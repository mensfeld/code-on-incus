"""
Test for `coi close` — the alias for `coi shutdown` (#593).

`close` mirrors the in-container `close` wrapper (which powers the container
off from inside): on the host, `coi close <name>` is `coi shutdown <name>`.
These assertions are incus-free — they prove the alias ROUTES to the
shutdown command (arg validation + help), not that a container is stopped.
"""

import subprocess


def test_close_routes_to_shutdown(coi_binary):
    """`coi close` with no target hits shutdown's own arg validation, not an
    'unknown command' error — proving the alias resolves to shutdownCommand."""
    result = subprocess.run(
        [coi_binary, "close"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode != 0, "coi close with no target should fail like coi shutdown"
    err = (result.stderr + result.stdout).lower()
    assert "no container names" in err, (
        f"coi close must reach shutdown's arg validation, not 'unknown command'. Got:\n{err}"
    )
    assert "unknown command" not in err, f"coi close must be a known alias. Got:\n{err}"


def test_close_help_is_shutdown_help(coi_binary):
    """`coi close --help` shows the shutdown command's help."""
    result = subprocess.run(
        [coi_binary, "close", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, f"coi close --help should succeed. stderr:\n{result.stderr}"
    out = result.stdout + result.stderr
    assert "Gracefully stop and delete" in out, f"should show shutdown help. Got:\n{out}"


def test_shutdown_help_lists_close_alias(coi_binary):
    """`coi shutdown --help` advertises the close alias (cobra Aliases line)."""
    result = subprocess.run(
        [coi_binary, "shutdown", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, f"coi shutdown --help should succeed. stderr:\n{result.stderr}"
    out = result.stdout + result.stderr
    assert "close" in out, f"shutdown help should list the 'close' alias. Got:\n{out}"
