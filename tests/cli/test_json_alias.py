"""
Integration tests for the universal --json flag (alias for --format json).

Every command that offers --format text|json also accepts --json; `container
exec` (--format json|raw) is deliberately excluded. These drive the real binary.
"""

import json
import subprocess

import pytest

# Each entry is the argv (after the binary) for a command that must accept --json.
# The flag-recognition check only asserts the flag PARSES (no "unknown flag"),
# so commands that then fail for other reasons (missing container, no incus) are
# fine — we're testing flag wiring, not the command's full behavior.
JSON_COMMANDS = [
    ["version"],
    ["profile", "list"],
    ["image", "list"],
    ["image", "info", "coi-default"],
    ["tmux", "list"],
    ["container", "info", "nonexistent-xyz"],
    ["container", "list"],
    ["list"],
    ["snapshot", "list", "--all"],
    ["validate", "profile", "nonexistent-xyz"],
]
# health omitted here on purpose: `coi health --json` runs the full (container-
# launching) health suite, too heavy just to check flag parsing. Its --json flag
# is guaranteed by the in-package TestFormatCommandsHaveJSONAlias tree walk.


@pytest.mark.parametrize("argv", JSON_COMMANDS, ids=lambda a: " ".join(a))
def test_json_flag_recognized(coi_binary, argv):
    """`coi <cmd> --json` must be a recognized flag (never 'unknown flag')."""
    r = subprocess.run([coi_binary, *argv, "--json"], capture_output=True, text=True, timeout=60)
    combined = (r.stdout + r.stderr).lower()
    assert "unknown flag" not in combined, f"`{' '.join(argv)} --json` not recognized:\n{combined}"


def test_version_json_equals_format_json(coi_binary):
    """`--json` is a true alias: identical output to `--format json`, valid JSON."""
    a = subprocess.run(
        [coi_binary, "version", "--json"], capture_output=True, text=True, timeout=30
    )
    b = subprocess.run(
        [coi_binary, "version", "--format", "json"], capture_output=True, text=True, timeout=30
    )
    assert a.returncode == 0 and b.returncode == 0, (a.stderr, b.stderr)
    assert a.stdout == b.stdout, f"--json and --format json diverged:\n{a.stdout!r}\n{b.stdout!r}"
    json.loads(a.stdout)  # must be valid JSON


def test_json_wins_over_conflicting_format_text(coi_binary):
    """When both are given, --json wins over --format text (matches monitor)."""
    r = subprocess.run(
        [coi_binary, "version", "--json", "--format", "text"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert r.returncode == 0, r.stderr
    json.loads(r.stdout)  # --json forced JSON despite --format text


def test_profile_list_json_equals_format_json(coi_binary, workspace_dir):
    """profile list (no incus needed): --json output equals --format json."""
    common = ["profile", "list", "--workspace", workspace_dir]
    a = subprocess.run(
        [coi_binary, *common, "--json"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    b = subprocess.run(
        [coi_binary, *common, "--format", "json"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert a.returncode == 0 and b.returncode == 0, (a.stderr, b.stderr)
    assert a.stdout == b.stdout, "profile list --json diverged from --format json"
    json.loads(a.stdout)


def test_container_exec_has_no_json_alias(coi_binary):
    """`container exec` uses --format json|raw, so it must NOT gain a --json alias."""
    r = subprocess.run(
        [coi_binary, "container", "exec", "somectr", "--json", "--", "true"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert "unknown flag" in (r.stdout + r.stderr).lower(), (
        "container exec should not accept --json (its format is json|raw)"
    )
