"""
Validation tests for `coi run` headless prompt mode (#701).

These exercise the flag/argument validation that happens BEFORE any container
work, so they run without Incus: mutual exclusion of the prompt flags, rejecting
a positional command alongside a prompt, an empty prompt, and an unknown
--prompt-name. All must exit with code 2 (a usage error, distinct from an agent
failure).
"""

import os
import subprocess


def _run(coi_binary, workspace_dir, *args, env=None):
    return subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, *args],
        capture_output=True,
        text=True,
        timeout=60,
        env=env,
    )


def test_prompt_flags_mutually_exclusive(coi_binary, workspace_dir):
    r = _run(coi_binary, workspace_dir, "--prompt", "x", "--prompt-name", "y")
    assert r.returncode == 2, f"want exit 2, got {r.returncode}: {r.stderr}"
    assert "mutually exclusive" in (r.stdout + r.stderr)


def test_prompt_with_positional_rejected(coi_binary, workspace_dir):
    r = _run(coi_binary, workspace_dir, "--prompt", "x", "--", "echo", "hi")
    assert r.returncode == 2, f"want exit 2, got {r.returncode}: {r.stderr}"
    assert "positional command cannot be combined" in (r.stdout + r.stderr)


def test_empty_prompt_rejected(coi_binary, workspace_dir):
    r = _run(coi_binary, workspace_dir, "--prompt", "   ")
    assert r.returncode == 2, f"want exit 2, got {r.returncode}: {r.stderr}"
    assert "empty" in (r.stdout + r.stderr).lower()


def test_unknown_prompt_name_rejected(coi_binary, workspace_dir, tmp_path):
    cfg = tmp_path / "coi-config.toml"
    cfg.write_text('[prompts]\nquick = "say hello"\n')
    env = os.environ.copy()
    env["COI_CONFIG"] = str(cfg)

    r = _run(coi_binary, workspace_dir, "--prompt-name", "nope", env=env)
    assert r.returncode == 2, f"want exit 2, got {r.returncode}: {r.stderr}"
    out = r.stdout + r.stderr
    assert 'no prompt named "nope"' in out
    # The error lists the available names so a typo is easy to fix.
    assert "quick" in out


def test_missing_prompt_file_rejected(coi_binary, workspace_dir):
    r = _run(coi_binary, workspace_dir, "--prompt-file", "/no/such/prompt.md")
    assert r.returncode == 2, f"want exit 2, got {r.returncode}: {r.stderr}"
    assert "failed to read --prompt-file" in (r.stdout + r.stderr)


def test_untrusted_project_prompt_is_ignored(coi_binary, workspace_dir):
    """A [prompts] entry in an untrusted project .coi/config.toml must be ignored
    entirely — prompts are honored only from trusted scope (~/.coi / $COI_CONFIG),
    so a cloned repo can't define a prompt the user then invokes by name (#701).
    Running from the workspace makes its .coi/config.toml the (untrusted) project
    config; --prompt-name must then fail to resolve it."""
    coi_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(coi_dir, exist_ok=True)
    with open(os.path.join(coi_dir, "config.toml"), "w") as f:
        f.write('[prompts]\nprojectonly = "should be ignored"\n')

    result = subprocess.run(
        [coi_binary, "run", "--prompt-name", "projectonly"],
        cwd=workspace_dir,
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 2, f"want exit 2, got {result.returncode}: {result.stderr}"
    out = result.stdout + result.stderr
    assert 'no prompt named "projectonly"' in out, out
