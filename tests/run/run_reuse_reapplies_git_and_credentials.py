"""
Test that a REUSED persistent `coi run` re-applies git identity + credentials.

Regression test for the #726 follow-up review (#1/#2): git identity and
[[credentials]] were originally applied only on fresh launches, so a reused
persistent container silently kept stale values — unlike `coi shell`, which
re-applies both on reuse. This creates a persistent container with one identity
+ credential, then reuses it with CHANGED config and asserts the reused run
reflects the new values.
"""

import os
import subprocess

from support.helpers import calculate_container_name


def _write_cfg(path, name, cred_host):
    path.write_text(
        "[container]\n"
        "persistent = true\n\n"
        "[git]\n"
        f'name = "{name}"\n'
        'email = "ident@example.com"\n\n'
        "[[credentials]]\n"
        f'host = "{cred_host}"\n'
        'container = "/home/code/.coi-test-cred"\n'
    )


def test_run_reuse_reapplies_git_and_credentials(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """A reused persistent run must reflect git/credential config changed since creation."""
    slot = 5
    container_name = calculate_container_name(workspace_dir, slot)

    secret_a = tmp_path / "cred_a.txt"
    secret_a.write_text("CRED_VALUE_A")
    secret_b = tmp_path / "cred_b.txt"
    secret_b.write_text("CRED_VALUE_B")

    cfg_a = tmp_path / "cfg_a.toml"
    cfg_b = tmp_path / "cfg_b.toml"
    _write_cfg(cfg_a, "First Ident", secret_a)
    _write_cfg(cfg_b, "Second Ident", secret_b)

    try:
        # === First run: create the persistent container with the "A" config ===
        env_a = os.environ.copy()
        env_a["COI_CONFIG"] = str(cfg_a)
        first = subprocess.run(
            [coi_binary, "run", "--workspace", workspace_dir, "--slot", str(slot), "--", "true"],
            capture_output=True,
            text=True,
            timeout=180,
            env=env_a,
        )
        assert first.returncode == 0, f"first run should succeed. stderr: {first.stderr}"

        # === Second run: REUSE the container with the changed "B" config ===
        env_b = os.environ.copy()
        env_b["COI_CONFIG"] = str(cfg_b)
        second = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--slot",
                str(slot),
                "--",
                "sh",
                "-c",
                "git config --global user.name; cat /home/code/.coi-test-cred",
            ],
            capture_output=True,
            text=True,
            timeout=180,
            env=env_b,
        )
        assert second.returncode == 0, f"second (reuse) run should succeed. stderr: {second.stderr}"

        # It must actually be a reuse, not a fresh recreate.
        assert "existing" in second.stderr.lower() or "restart" in second.stderr.lower(), (
            f"second run should reuse the persistent container. stderr:\n{second.stderr}"
        )
        # ...and reflect the CHANGED identity + credential (the fix).
        assert "Second Ident" in second.stdout, (
            f"reused run should re-apply the updated git identity. stdout:\n{second.stdout}"
        )
        assert "CRED_VALUE_B" in second.stdout, (
            f"reused run should re-seed the updated credential. stdout:\n{second.stdout}"
        )
    finally:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
            check=False,
        )
