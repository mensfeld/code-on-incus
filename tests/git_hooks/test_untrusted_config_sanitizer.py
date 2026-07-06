"""
Untrusted project config cannot turn off read-only protections (H4-class:
a cloned/agent-planted repo must not be able to make host-auto-executing files
writable inside the container).

The sanitizer (`sanitizeUntrustedSecurity` / `sanitizeUntrustedGit` in
internal/config/loader.go) strips every protection-weakening field from a
project `.coi/config.toml`, honoring it only from trusted scope
(~/.coi/config.toml or $COI_CONFIG). These e2e tests prove each vector end to
end: a project config sets the weakening field, and a default protected path
(`.git/hooks`) stays read-only inside the container anyway. Each also asserts
the actionable downgrade warning is emitted.

Covers CHANGELOG's "Every protection-weakening config field is now trusted-only"
claim (#559 A1). writable_paths-from-untrusted is already covered by
tests/mount/test_claude_settings_protection.py.
"""

import subprocess
from pathlib import Path

# The default protected set includes .git/hooks; a container write there is the
# canonical "host-auto-executing file" test — a hook that runs on the host at the
# next git op.
PROTECTED_TARGET = "/workspace/.git/hooks/pre-commit"


def _init_repo(workspace_dir):
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)
    hooks = Path(workspace_dir) / ".git" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    # A real file to attempt to overwrite; its content must survive.
    (hooks / "pre-commit").write_text("#!/bin/sh\nexit 0\n")


def _write_project_config(workspace_dir, body):
    cfg = Path(workspace_dir) / ".coi"
    cfg.mkdir(exist_ok=True)
    (cfg / "config.toml").write_text(body)


def _attempt_write(coi_binary, workspace_dir):
    return subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            f"echo pwned > {PROTECTED_TARGET}",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )


def _assert_still_protected(result, workspace_dir, field):
    combined = (result.stdout + result.stderr).lower()
    # The container write must fail (read-only mount still in place)...
    assert result.returncode != 0 or "read-only" in combined or "read only" in combined, (
        f"{field} from an untrusted project config was honored — the protected "
        f"path became writable.\nrc={result.returncode}\n{combined}"
    )
    # ...the host file must be unchanged...
    host_hook = Path(workspace_dir) / ".git" / "hooks" / "pre-commit"
    assert host_hook.read_text() == "#!/bin/sh\nexit 0\n", (
        f"{field}: the protected host file was modified from inside the container"
    )
    # ...and the downgrade was announced, not silently dropped.
    assert "ignoring" in combined and field.lower() in combined, (
        f"{field}: expected an 'ignoring <field>' downgrade warning.\n{combined}"
    )


def test_untrusted_disable_protection_ignored(coi_binary, cleanup_containers, workspace_dir):
    """[security] disable_protection = true from a project config is stripped."""
    _init_repo(workspace_dir)
    _write_project_config(workspace_dir, "[security]\ndisable_protection = true\n")
    _assert_still_protected(
        _attempt_write(coi_binary, workspace_dir), workspace_dir, "disable_protection"
    )


def test_untrusted_protected_paths_replace_ignored(coi_binary, cleanup_containers, workspace_dir):
    """[security] protected_paths = [<non-default>] is a full replace that would
    DROP .git/hooks from the protected set; the sanitizer strips it (a non-empty
    list, so it trips the len>0 downgrade guard) and the defaults hold."""
    _init_repo(workspace_dir)
    _write_project_config(workspace_dir, '[security]\nprotected_paths = ["docs"]\n')
    _assert_still_protected(
        _attempt_write(coi_binary, workspace_dir), workspace_dir, "protected_paths"
    )


def test_untrusted_host_immutable_false_ignored(coi_binary, cleanup_containers, workspace_dir):
    """[security] host_immutable = false from a project config is stripped (the
    read-only mount holds regardless, and the downgrade is warned)."""
    _init_repo(workspace_dir)
    _write_project_config(workspace_dir, "[security]\nhost_immutable = false\n")
    _assert_still_protected(
        _attempt_write(coi_binary, workspace_dir), workspace_dir, "host_immutable"
    )


def test_untrusted_writable_hooks_ignored(coi_binary, cleanup_containers, workspace_dir):
    """[git] writable_hooks = true from a project config is stripped — .git/hooks
    stays read-only inside the container."""
    _init_repo(workspace_dir)
    _write_project_config(workspace_dir, "[git]\nwritable_hooks = true\n")
    _assert_still_protected(
        _attempt_write(coi_binary, workspace_dir), workspace_dir, "writable_hooks"
    )
