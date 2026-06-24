"""
Workspace secret-path masking must hide repo-local secrets from the agent.

`security.secret_paths` lists workspace-relative globs (e.g. `.env`, `*.pem`,
`secrets/**`). COI masks each match inside the container by mounting an empty,
read-only file/dir over it, so the contained agent can neither READ its contents
(exfil protection) nor MODIFY it (tamper protection). The host file is untouched.
Issue #494.
"""

import subprocess
from pathlib import Path

SECRET_ENV = "API_TOKEN=topsecret-do-not-leak"
SECRET_PEM = "-----BEGIN PRIVATE KEY-----leakme"
SECRET_DB = "db-password-leakme"


def _run(coi_binary, workspace_dir, argv, timeout=120):
    return subprocess.run(
        [coi_binary, "run", "--", *argv],
        capture_output=True,
        text=True,
        timeout=timeout,
        cwd=workspace_dir,
    )


def _seed(workspace_dir):
    ws = Path(workspace_dir)
    (ws / ".env").write_text(SECRET_ENV + "\n")
    (ws / "deploy.pem").write_text(SECRET_PEM + "\n")
    secrets = ws / "secrets"
    secrets.mkdir(exist_ok=True)
    (secrets / "db.conf").write_text(SECRET_DB + "\n")
    # Project config requesting masking. secret_paths is additive, so it is
    # honored even from an (untrusted) project .coi/config.toml.
    coi = ws / ".coi"
    coi.mkdir(exist_ok=True)
    (coi / "config.toml").write_text('[security]\nsecret_paths = [".env", "*.pem", "secrets/**"]\n')


def test_secret_paths_hidden_from_agent(coi_binary, cleanup_containers, workspace_dir):
    """The agent reads EMPTY for masked files/dirs — never the secret content."""
    _seed(workspace_dir)

    # .env and *.pem are masked with an empty file: cat returns nothing.
    for path, secret in (
        ("/workspace/.env", "topsecret-do-not-leak"),
        ("/workspace/deploy.pem", "leakme"),
    ):
        r = _run(coi_binary, workspace_dir, ["cat", path])
        assert secret not in r.stdout and secret not in r.stderr, (
            f"masked {path} leaked its secret to the agent.\nstdout: {r.stdout}\nstderr: {r.stderr}"
        )

    # secrets/ is masked with an empty dir: db.conf is not visible.
    r = _run(
        coi_binary,
        workspace_dir,
        ["sh", "-c", "ls -A /workspace/secrets; cat /workspace/secrets/db.conf 2>&1 || true"],
    )
    combined = r.stdout + r.stderr
    assert "db.conf" not in combined and "leakme" not in combined, (
        f"masked secrets/ leaked its contents to the agent.\n{combined}"
    )


def test_secret_paths_readonly(coi_binary, cleanup_containers, workspace_dir):
    """Masked secret files cannot be modified (read-only mount)."""
    _seed(workspace_dir)

    modify = _run(
        coi_binary,
        workspace_dir,
        ["sh", "-c", "echo 'pwned' > /workspace/.env"],
    )
    combined = (modify.stdout + modify.stderr).lower()
    assert modify.returncode != 0 or "read-only" in combined, (
        f"writing a masked secret should fail (read-only).\n"
        f"returncode: {modify.returncode}\nstdout: {modify.stdout}\nstderr: {modify.stderr}"
    )


def test_secret_paths_host_file_untouched(coi_binary, cleanup_containers, workspace_dir):
    """Masking only affects the container view; the host secret is unchanged."""
    _seed(workspace_dir)

    # Run anything so masking is applied for a session.
    _run(coi_binary, workspace_dir, ["true"])

    # The real secret is still on the host (masking does not delete it).
    assert (Path(workspace_dir) / ".env").read_text().strip() == SECRET_ENV
    assert (Path(workspace_dir) / "secrets" / "db.conf").read_text().strip() == SECRET_DB
