"""
Test for `coi close` — the accepted alias for `coi shutdown` (#593).

These assertions are incus-free: they prove the alias is REGISTERED and that
it routes to the shutdown command (not to `kill`, which shares the same
container-arg helper and would emit the same "no container names" error).
"""

import re
import subprocess


def test_close_routes_to_shutdown(coi_binary):
    """`coi close` with no target reaches the container-arg validation rather
    than an 'unknown command' error — i.e. the alias is registered and runs.
    (test_close_help_is_shutdown_help proves it is shutdown, not kill.)"""
    result = subprocess.run(
        [coi_binary, "close"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode != 0, "coi close with no target should fail like coi shutdown"
    out = result.stderr + result.stdout
    assert "unknown command" not in out.lower(), f"coi close must be a known alias. Got:\n{out}"
    assert "no container names" in out.lower(), (
        f"coi close must reach the container-arg validation. Got:\n{out}"
    )


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
    # This is the discriminator: `kill` shares the same container-arg helper and
    # error strings, so only the resolved command's OWN help proves which one ran.
    assert "Gracefully stop and delete" in out, f"should show shutdown help. Got:\n{out}"
    assert "Force stop and delete containers immediately" not in out, (
        f"coi close must resolve to shutdown, NOT kill. Got:\n{out}"
    )


def test_shutdown_help_lists_close_alias(coi_binary):
    """`coi shutdown --help` carries cobra's Aliases block naming close.

    Asserting on the Aliases BLOCK (not the bare word "close", which also
    appears in the help prose) is what actually proves the alias is
    registered — a prose-only match would still pass if Aliases were dropped.
    """
    result = subprocess.run(
        [coi_binary, "shutdown", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, f"coi shutdown --help should succeed. stderr:\n{result.stderr}"
    out = result.stdout + result.stderr
    assert re.search(r"Aliases:\s*\n\s*shutdown,\s*close", out), (
        f"help must carry cobra's 'Aliases: shutdown, close' block. Got:\n{out}"
    )


def test_clos_typo_suggests_close(coi_binary):
    """The near-miss `coi clos` points at this command, not an unrelated one
    (cobra's suggestions ignore aliases, hence SuggestFor)."""
    result = subprocess.run(
        [coi_binary, "clos"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode != 0, "coi clos is not a command and must fail"
    out = result.stderr + result.stdout
    assert "shutdown" in out, (
        f"the 'clos' typo should suggest the shutdown/close command. Got:\n{out}"
    )
