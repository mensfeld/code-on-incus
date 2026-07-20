"""
Integration tests for [defaults] profile (#607).

`[defaults] profile = "name"` selects the profile a bare `coi` uses when
`--profile` is not passed, so a user's opinionated setup applies without retyping
it — while `coi --profile default` still resolves to the clean clone of global
config. It is the lowest-precedence profile source (explicit --profile wins),
honored from trusted scope only, and a name that resolves to no known profile is
a hard error at startup.

These tests assert *which* profile is effective without needing a successful
container launch: `coi run` resolves the image from the effective config and,
finding a per-profile marker image that does not exist, aborts with
`image '<image>' not found` (non-interactive stdin never triggers a build). That
marker image name reveals which profile was resolved.
"""

import subprocess
from pathlib import Path

from support.helpers import write_trusted_coi_config

# Distinct marker images so the resolved profile is unambiguous in the output.
P_IMAGE = "coi-defaultprofile-p"
OTHER_IMAGE = "coi-defaultprofile-other"
VANILLA_IMAGE = "coi-defaultprofile-vanilla"


def _make_profile(workspace_dir, name, image):
    """Create <workspace>/.coi/profiles/<name>/config.toml pinning a marker image."""
    pdir = Path(workspace_dir) / ".coi" / "profiles" / name
    pdir.mkdir(parents=True, exist_ok=True)
    (pdir / "config.toml").write_text(f'[container]\nimage = "{image}"\n')


def _run(coi_binary, workspace_dir, args, env):
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, *args, "--", "true"],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
        env=env,
    )
    return result.stdout + result.stderr


def test_default_profile_applied_when_no_flag(coi_binary, cleanup_containers, workspace_dir):
    """Bare `coi run` resolves to [defaults] profile."""
    _make_profile(workspace_dir, "p", P_IMAGE)
    env = write_trusted_coi_config(
        f'[container]\nimage = "{VANILLA_IMAGE}"\n[defaults]\nprofile = "p"\n'
    )

    output = _run(coi_binary, workspace_dir, [], env)
    assert P_IMAGE in output, (
        f"a bare `coi run` should apply [defaults] profile 'p' (image {P_IMAGE}). Got:\n{output}"
    )
    assert VANILLA_IMAGE not in output, (
        f"the un-profiled global image {VANILLA_IMAGE} must not be used. Got:\n{output}"
    )


def test_profile_default_flag_gives_vanilla(coi_binary, cleanup_containers, workspace_dir):
    """`coi --profile default` bypasses [defaults] profile and gives the clean clone."""
    _make_profile(workspace_dir, "p", P_IMAGE)
    env = write_trusted_coi_config(
        f'[container]\nimage = "{VANILLA_IMAGE}"\n[defaults]\nprofile = "p"\n'
    )

    output = _run(coi_binary, workspace_dir, ["--profile", "default"], env)
    assert VANILLA_IMAGE in output, (
        f"`--profile default` should resolve to the global-config clone (image "
        f"{VANILLA_IMAGE}). Got:\n{output}"
    )
    assert P_IMAGE not in output, (
        f"`--profile default` must not apply [defaults] profile 'p'. Got:\n{output}"
    )


def test_explicit_profile_overrides_default(coi_binary, cleanup_containers, workspace_dir):
    """An explicit `--profile other` wins over [defaults] profile."""
    _make_profile(workspace_dir, "p", P_IMAGE)
    _make_profile(workspace_dir, "other", OTHER_IMAGE)
    env = write_trusted_coi_config(
        f'[container]\nimage = "{VANILLA_IMAGE}"\n[defaults]\nprofile = "p"\n'
    )

    output = _run(coi_binary, workspace_dir, ["--profile", "other"], env)
    assert OTHER_IMAGE in output, (
        f"explicit `--profile other` should win (image {OTHER_IMAGE}). Got:\n{output}"
    )
    assert P_IMAGE not in output, (
        f"[defaults] profile 'p' must not apply when --profile is given. Got:\n{output}"
    )


def test_nonexistent_default_profile_hard_errors(coi_binary, cleanup_containers, workspace_dir):
    """A [defaults] profile naming an unknown profile fails fast at startup."""
    env = write_trusted_coi_config('[defaults]\nprofile = "ghost"\n')

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "true"],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
        env=env,
    )
    combined = result.stdout + result.stderr
    assert result.returncode != 0, f"an invalid [defaults] profile must fail. Got:\n{combined}"
    assert "ghost" in combined, f"the error should name the missing profile. Got:\n{combined}"
    assert "Launching container" not in combined, (
        f"it must fail before any container work. Got:\n{combined}"
    )


def test_profile_list_works_with_invalid_default_profile(
    coi_binary, cleanup_containers, workspace_dir
):
    """Introspection commands stay usable despite an invalid [defaults] profile."""
    env = write_trusted_coi_config('[defaults]\nprofile = "ghost"\n')

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
        env=env,
    )
    combined = result.stdout + result.stderr
    assert result.returncode == 0, (
        f"`coi profile list` must stay usable so a bad default can be diagnosed. Got:\n{combined}"
    )


def test_untrusted_project_default_profile_ignored(coi_binary, cleanup_containers, workspace_dir):
    """A project ./.coi/config.toml cannot set the default profile (downgrade guard)."""
    _make_profile(workspace_dir, "p", P_IMAGE)
    # Untrusted project config tries to redirect the default to 'p'.
    project_cfg = Path(workspace_dir) / ".coi" / "config.toml"
    project_cfg.parent.mkdir(parents=True, exist_ok=True)
    project_cfg.write_text('[defaults]\nprofile = "p"\n')

    # Trusted config sets only the (marker) base image, no default profile.
    env = write_trusted_coi_config(f'[container]\nimage = "{VANILLA_IMAGE}"\n')

    output = _run(coi_binary, workspace_dir, [], env)
    assert "ignoring '[defaults] profile'" in output, (
        f"an untrusted project default profile should be reported as ignored. Got:\n{output}"
    )
    assert P_IMAGE not in output, (
        f"the project-scoped default profile 'p' must not take effect. Got:\n{output}"
    )
    assert VANILLA_IMAGE in output, (
        f"with the project default ignored and no trusted default, the base image "
        f"{VANILLA_IMAGE} applies. Got:\n{output}"
    )
