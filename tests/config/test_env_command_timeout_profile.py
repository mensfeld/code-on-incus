"""
A profile can carry env_command_timeout (parity fix), honored end-to-end through
the coi binary — and, when untrusted, stripped like env_commands.

env_command_timeout used to be settable only under top-level [defaults]; a
profile could set env_commands but not the timeout that bounds them. These tests
are deterministic (no container — `coi profile info` only loads + prints):

1. A TRUSTED profile (~/.coi/profiles, via a fake HOME) keeps env_command_timeout
   and `coi profile info` echoes it — proving the field parses from real TOML,
   passes JSON-schema validation, and is retained on the ProfileConfig.
2. An UNTRUSTED, project-scoped profile (./.coi/profiles) has a lone
   env_command_timeout stripped at load, so it is NOT echoed — a cloned repo
   cannot shorten/extend the timeout applied to trusted-scope env_commands.

The apply/inherit/strip behavior is additionally covered by Go unit tests in
internal/config (TestApplyProfile_EnvCommandTimeout et al.).

Regression note: the key MUST live at the profile root, before any [table]
header — otherwise TOML scopes it into that table (e.g. container.*), where it is
an unknown key and schema validation rejects the profile.
"""

import os
import subprocess
from pathlib import Path

# env_command_timeout is a profile-ROOT key: keep it above every [table].
PROFILE = 'env_command_timeout = "7s"\n\n[container]\nimage = "coi-default"\n'


def _env_with_home(fake_home: Path) -> dict:
    env = os.environ.copy()
    env["HOME"] = str(fake_home)
    env.pop("COI_CONFIG", None)  # don't let a stray COI_CONFIG add trusted scope
    return env


def _profile_info(coi_binary, cwd, home, name):
    result = subprocess.run(
        [coi_binary, "profile", "info", name, "--workspace", str(cwd)],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=str(cwd),
        env=_env_with_home(home),
    )
    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    return result.stdout + result.stderr


def test_trusted_profile_keeps_env_command_timeout(coi_binary, cleanup_containers, tmp_path):
    """A trusted ~/.coi profile retains env_command_timeout; info echoes it."""
    home = tmp_path / "home"
    (home / ".coi" / "profiles" / "slow").mkdir(parents=True, exist_ok=True)
    (home / ".coi" / "profiles" / "slow" / "config.toml").write_text(PROFILE)
    workspace = tmp_path / "workspace"
    workspace.mkdir(exist_ok=True)

    out = _profile_info(coi_binary, workspace, home, "slow")
    assert 'env_command_timeout = "7s"' in out, (
        f"trusted profile's env_command_timeout should be echoed. Got:\n{out}"
    )


def test_untrusted_profile_strips_lone_env_command_timeout(
    coi_binary, cleanup_containers, tmp_path
):
    """A project-scoped (untrusted) profile has a lone env_command_timeout stripped."""
    home = tmp_path / "home"
    (home / ".coi").mkdir(parents=True, exist_ok=True)  # empty trusted scope, no profile here
    workspace = tmp_path / "workspace"
    (workspace / ".coi" / "profiles" / "slow").mkdir(parents=True, exist_ok=True)
    (workspace / ".coi" / "profiles" / "slow" / "config.toml").write_text(PROFILE)

    out = _profile_info(coi_binary, workspace, home, "slow")
    assert "env_command_timeout" not in out, (
        "an untrusted profile's env_command_timeout must be stripped, so profile "
        f"info must not echo it. Got:\n{out}"
    )
