"""
Regression repro for #623: `coi run -- <cmd>` executes with no $HOME.

`coi run` builds its container-exec environment in `appendEnvArgs`
(internal/cli/run.go): it passes TZ, forwarded sockets, `[defaults].environment`
and `forward_env`, but never HOME (or USER). `incus exec` does not synthesize
HOME for a `--user` exec, so the command lands with no HOME — and anything that
resolves `~` or reads `--global` config breaks (`git config --global` fails with
`fatal: $HOME not set`). `coi shell` DOES set HOME (buildContainerEnv), so the
two paths are inconsistent.

These tests assert the EXPECTED behavior (HOME present; `git config --global`
works). They are marked xfail(strict) because the fix is not in yet:
  * today they FAIL — which reproduces/documents the bug in CI as XFAIL while
    keeping the suite green;
  * once HOME is wired into appendEnvArgs they will pass, and strict xfail turns
    that XPASS into a failure — a built-in reminder to drop this marker in the
    fix PR (at which point these become ordinary passing regression tests).
"""

import subprocess

import pytest

# The container's code user home; matches `coi shell`'s HOME (/home/<code_user>)
# and the default image's `useradd -m` home for the `code` user.
EXPECTED_HOME = "/home/code"

pytestmark = pytest.mark.xfail(
    strict=True,
    reason="#623: coi run omits HOME from the exec env (appendEnvArgs); fix pending",
)


def test_run_sets_home(coi_binary, cleanup_containers, workspace_dir):
    """`coi run` should expose HOME=/home/code, like an interactive session does."""
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            'echo "COI_HOME=[$HOME]"',
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    combined = result.stdout + result.stderr
    assert f"COI_HOME=[{EXPECTED_HOME}]" in combined, (
        f"coi run should set HOME={EXPECTED_HOME} (it is currently empty). Got:\n{combined}"
    )


def test_run_git_config_global_works(coi_binary, cleanup_containers, workspace_dir):
    """`git config --global` under `coi run` must not fail with '$HOME not set'."""
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "git",
            "config",
            "--global",
            "user.email",
            "coi@example.com",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )
    combined = result.stdout + result.stderr
    assert "HOME not set" not in combined, (
        f"git --global broke because coi run set no HOME (#623). Got:\n{combined}"
    )
    assert result.returncode == 0, (
        f"git config --global should succeed under coi run. Got exit {result.returncode}:\n{combined}"
    )
