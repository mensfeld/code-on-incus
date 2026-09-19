"""
Test that `coi run` configures the git commit identity + guard.

`coi shell` seeded the git identity ([git] name/email) and set
user.useConfigOnly=true so an agent can't commit as the default "code" user;
`coi run` did neither. This asserts `coi run` now applies both (#726 follow-up).
Uses trusted-scope config ($COI_CONFIG) since [git] name/email are stripped from
an untrusted project config.
"""

import os
import subprocess


def _git_config(coi_binary, workspace_dir, env, key):
    return subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "git",
            "config",
            "--global",
            key,
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
    )


def test_run_configures_git_identity(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """coi run must seed [git] name/email and enable the useConfigOnly guard."""
    cfg = tmp_path / "coi-config.toml"
    cfg.write_text('[git]\nname = "Run Tester"\nemail = "run-tester@example.com"\n')

    env = os.environ.copy()
    env["COI_CONFIG"] = str(cfg)

    name = _git_config(coi_binary, workspace_dir, env, "user.name")
    assert name.returncode == 0, f"coi run should succeed. stderr: {name.stderr}"
    assert name.stdout.strip() == "Run Tester", (
        f"git user.name should be seeded from config, got: {name.stdout.strip()!r}"
    )

    email = _git_config(coi_binary, workspace_dir, env, "user.email")
    assert email.stdout.strip() == "run-tester@example.com", (
        f"git user.email should be seeded from config, got: {email.stdout.strip()!r}"
    )

    guard = _git_config(coi_binary, workspace_dir, env, "user.useConfigOnly")
    assert guard.stdout.strip() == "true", (
        f"git user.useConfigOnly guard should be enabled, got: {guard.stdout.strip()!r}"
    )
