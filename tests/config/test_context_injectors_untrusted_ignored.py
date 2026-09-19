"""
Security e2e: an untrusted project .coi/config.toml must NOT be able to inject an
arbitrary HOST file into the container via [tool] context_file / context_json_file
(#705 review). Both read a host path and write it where the in-container agent can
read it, so a cloned repo could point them at a host secret (~/.ssh/id_rsa, ...).

conftest sets COI_TRUST_ALL=1 suite-wide (so most tests' out-of-workspace configs
are trusted); this test builds a fully controlled env WITHOUT COI_TRUST_ALL so the
untrusted gate is active, then asserts the injectors are stripped: the container
gets the GENERATED context files, not the host "secret" the project config pointed
at.
"""

import json
import os
import subprocess

SENTINEL_MD = "SENTINEL-UNTRUSTED-CONTEXT-MD"
SENTINEL_JSON = "SENTINEL-UNTRUSTED-CONTEXT-JSON"


def _incus_cat(container_name, path):
    r = subprocess.run(
        ["sg", "incus-admin", "-c", f"incus exec {container_name} -- cat {path}"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    return r.stdout if r.returncode == 0 else ""


def test_untrusted_context_injectors_ignored(
    coi_binary, workspace_dir, cleanup_containers, tmp_path
):
    # Host "secret" files a malicious project config tries to smuggle in.
    evil_md = tmp_path / "evil.md"
    evil_md.write_text(SENTINEL_MD)
    evil_json = tmp_path / "evil.json"
    evil_json.write_text(json.dumps({"marker": SENTINEL_JSON}))

    # Untrusted project config (as if from a cloned repo) pointing both injectors
    # at those host files.
    coi_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(coi_dir, exist_ok=True)
    with open(os.path.join(coi_dir, "config.toml"), "w") as f:
        f.write(f'[tool]\ncontext_file = "{evil_md}"\ncontext_json_file = "{evil_json}"\n')

    # Fully controlled env WITHOUT COI_TRUST_ALL so the untrusted gate is armed.
    env = {k: v for k, v in os.environ.items() if k != "COI_TRUST_ALL"}
    env["COI_USE_DUMMY"] = "1"
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    claude_dir = fake_home / ".claude"
    claude_dir.mkdir()
    (claude_dir / ".credentials.json").write_text('{"token": "test"}')
    env["HOME"] = str(fake_home)

    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background"],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
        env=env,
    )
    assert result.returncode == 0, f"coi shell failed: {result.stdout}\n{result.stderr}"

    container_name = None
    for line in (result.stdout + result.stderr).split("\n"):
        if "Container: " in line:
            container_name = line.split("Container: ")[1].strip()
            break
    assert container_name, f"no container name in output: {result.stdout + result.stderr}"

    try:
        md = _incus_cat(container_name, "/home/code/SANDBOX_CONTEXT.md")
        js = _incus_cat(container_name, "/home/code/SANDBOX_CONTEXT.json")

        # The definitive security proof: the host "secrets" must NOT have been
        # injected (the injectors were stripped as untrusted)...
        assert SENTINEL_MD not in md, (
            "untrusted context_file leaked a host file into SANDBOX_CONTEXT.md"
        )
        assert SENTINEL_JSON not in js, (
            "untrusted context_json_file leaked a host file into SANDBOX_CONTEXT.json"
        )
        # ...the container got the GENERATED files instead.
        assert "COI Sandbox Environment" in md, f"expected the generated .md, got:\n{md[:400]}"
        assert '"schema_version"' in js, f"expected the generated .json, got:\n{js[:400]}"
    finally:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
