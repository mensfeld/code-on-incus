"""
Security e2e: an untrusted project .coi/config.toml must NOT be able to inject an
arbitrary HOST file into the container via [tool] context_file / context_json_file
(#705 review). Both read a host path and write it where the in-container agent can
read it, so a cloned repo could point them at a host secret (~/.ssh/id_rsa, ...).

conftest sets COI_TRUST_ALL=1 suite-wide (so most tests' out-of-workspace configs
are trusted); this test removes it so the untrusted gate is active, then asserts
the injectors are stripped: the container gets the GENERATED context files, not
the host "secret" the project config pointed at, plus a downgrade warning.
"""

import json
import os
import pathlib
import subprocess

SENTINEL_MD = "SENTINEL-UNTRUSTED-CONTEXT-MD"
SENTINEL_JSON = "SENTINEL-UNTRUSTED-CONTEXT-JSON"


def _env_gate_active():
    """Environment with COI_TRUST_ALL removed so the untrusted gate is armed."""
    return {k: v for k, v in os.environ.items() if k != "COI_TRUST_ALL"}


def test_untrusted_context_injectors_ignored(
    coi_binary, workspace_dir, cleanup_containers, tmp_path
):
    # Host "secret" files a malicious project config tries to smuggle in.
    evil_md = tmp_path / "evil.md"
    evil_md.write_text(SENTINEL_MD)
    evil_json = tmp_path / "evil.json"
    evil_json.write_text(json.dumps({"marker": SENTINEL_JSON}))

    coi_dir = pathlib.Path(workspace_dir) / ".coi"
    coi_dir.mkdir(exist_ok=True)
    (coi_dir / "config.toml").write_text(
        f'[tool]\ncontext_file = "{evil_md}"\ncontext_json_file = "{evil_json}"\n'
    )

    r = subprocess.run(
        [
            coi_binary,
            "run",
            "--",
            "sh",
            "-c",
            "cat /home/code/SANDBOX_CONTEXT.md; echo '===SPLIT==='; cat /home/code/SANDBOX_CONTEXT.json",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
        env=_env_gate_active(),
    )

    stderr = r.stderr.lower()
    assert "ignoring 'tool.context_file'" in stderr, (
        f"expected a downgrade warning for tool.context_file. stderr:\n{r.stderr}"
    )
    assert "ignoring 'tool.context_json_file'" in stderr, (
        f"expected a downgrade warning for tool.context_json_file. stderr:\n{r.stderr}"
    )

    out = r.stdout
    # The host "secrets" must NOT have been injected.
    assert SENTINEL_MD not in out, (
        "untrusted context_file was injected — host file leaked into container"
    )
    assert SENTINEL_JSON not in out, (
        "untrusted context_json_file was injected — host file leaked into container"
    )
    # The container got the GENERATED files instead.
    assert "COI Sandbox Environment" in out, f"expected the generated .md. stdout:\n{out[:600]}"
    assert '"schema_version"' in out, f"expected the generated .json. stdout:\n{out[:600]}"
