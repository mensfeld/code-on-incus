"""
Test that `coi run` seeds [[credentials]] into the container.

`coi shell` copied [[credentials]] entries (a generic host->container file-seeding
primitive, same class as [[mounts]]/[[sockets]] which run already honored) but
`coi run` dropped them. This asserts an ad-hoc credential is present inside a run
(#726 follow-up). Uses trusted-scope config ($COI_CONFIG) since ad-hoc credentials
are stripped from an untrusted project config.
"""

import os
import subprocess


def test_run_seeds_credentials(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """coi run must copy an ad-hoc [[credentials]] file into the container."""
    secret = tmp_path / "coi-test-cred.txt"
    content = "COI_RUN_CREDENTIAL_9f3a"
    secret.write_text(content)

    cfg = tmp_path / "coi-config.toml"
    cfg.write_text(f'[[credentials]]\nhost = "{secret}"\ncontainer = "/home/code/.coi-test-cred"\n')

    env = os.environ.copy()
    env["COI_CONFIG"] = str(cfg)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "cat",
            "/home/code/.coi-test-cred",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
    )
    assert result.returncode == 0, f"coi run should succeed. stderr: {result.stderr}"
    assert content in result.stdout, (
        f"credential file content should be present in the container. Got:\n{result.stdout}"
    )
