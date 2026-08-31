"""
Non-executing launch spec: `coi tool spec` (#751).

`coi tool spec` prints — without executing — the exact command + tool-derived
env for launching the profile's tool inside an existing container. An external
orchestrator then runs that command through its own container exec + tmux. coi
builds the command from the profile's tool (session id, resume, model,
permission) and stages the prompt into the container; the orchestrator owns
execution, so no coi-owned tmux/exit-file machinery is involved.

These assert the end-to-end contract with the dummy tool against a real
container:

1. `coi tool spec --json` prints {command, env} where command is the tool's argv
   (dummy-overridden binary) with the prompt embedded via "$(cat …)".
2. The prompt is staged into the container at ~/.coi/runs/<id>.prompt verbatim.
3. The profile's [tool.claude] model is emitted in env (model/effort only), and
   secrets are absent.
"""

import json
import os
import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
)


def _write_config(workspace_dir, tool="claude", model=None):
    config_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(config_dir, exist_ok=True)
    body = f'[container]\npersistent = true\n\n[tool]\nname = "{tool}"\n'
    if model:
        body += f'\n[tool.{tool}]\nmodel = "{model}"\n'
    with open(os.path.join(config_dir, "config.toml"), "w") as f:
        f.write(body)


def _boot_container(coi_binary, workspace_dir):
    """Boot a real container via `coi shell` (dummy tool) and return (child,
    container_name) once it's ready for `coi tool spec` to target."""
    container_name = calculate_container_name(workspace_dir, 1)
    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        env={"COI_USE_DUMMY": "1"},
        timeout=120,
    )
    wait_for_container_ready(child, timeout=90)
    time.sleep(3)
    return child, container_name


def _teardown(coi_binary, child, container_name):
    try:
        child.send("\x03")
        time.sleep(0.5)
        child.send("sudo poweroff")
        time.sleep(0.3)
        child.send("\x0d")
        try:
            child.expect(EOF, timeout=60)
        except TIMEOUT:
            pass
    finally:
        try:
            child.close(force=True)
        except Exception:
            pass
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )


def _cat_in_container(container, path):
    r = subprocess.run(
        ["incus", "exec", container, "--", "cat", path],
        capture_output=True,
        text=True,
        timeout=20,
    )
    return r.stdout + r.stderr


def _spec(
    coi_binary,
    container,
    workspace_dir,
    session_id,
    prompt_file=None,
    system_prompt_file=None,
    extra=None,
):
    argv = [
        coi_binary,
        "tool",
        "spec",
        "--container",
        container,
        "--session-id",
        session_id,
        "--json",
    ]
    if prompt_file is not None:
        argv += ["--prompt-file", str(prompt_file)]
    if system_prompt_file is not None:
        argv += ["--system-prompt-file", str(system_prompt_file)]
    if extra:
        argv += extra
    r = subprocess.run(
        argv,
        capture_output=True,
        text=True,
        timeout=60,
        env={**os.environ, "COI_USE_DUMMY": "1"},
        cwd=workspace_dir,
    )
    assert r.returncode == 0, f"tool spec failed. stdout:{r.stdout}\nstderr:{r.stderr}"
    return json.loads(r.stdout.strip().splitlines()[-1])


def test_tool_spec_command_and_prompt_staging(coi_binary, workspace_dir, cleanup_containers):
    """Core: spec prints {command, env}; the prompt is staged verbatim and
    referenced by argv via "$(cat …)"; env carries the profile model only."""
    _write_config(workspace_dir, model="coi-test-spec-opus")
    prompt_file = os.path.join(workspace_dir, "prompt.txt")
    prompt_text = "hello 'single' \"double\" $(whoami) && echo pwned\nline2\n"
    with open(prompt_file, "w") as f:
        f.write(prompt_text)

    child, container = _boot_container(coi_binary, workspace_dir)
    try:
        sid = "spec-session-1"
        spec = _spec(coi_binary, container, workspace_dir, sid, prompt_file)

        assert isinstance(spec.get("command"), list) and spec["command"], spec
        # Dummy override replaces the binary; the prompt is the trailing cat-subst.
        assert spec["command"][0] == "dummy", spec["command"]
        staged_ref = f'"$(cat /home/code/.coi/runs/{sid}.prompt)"'
        assert spec["command"][-1] == staged_ref, spec["command"]

        # Claude embeds the prompt in argv, so no out-of-band prompt field.
        assert "prompt" not in spec, spec

        # Env is tool-derived only (model), no secrets.
        assert spec["env"].get("ANTHROPIC_MODEL") == "coi-test-spec-opus", spec["env"]
        assert not any("TOKEN" in k or "KEY" in k for k in spec["env"]), spec["env"]

        # Prompt staged verbatim — no command substitution ran.
        staged = _cat_in_container(container, f"/home/code/.coi/runs/{sid}.prompt")
        assert "$(whoami)" in staged and "pwned" in staged and "line2" in staged, staged
    finally:
        _teardown(coi_binary, child, container)


def test_tool_spec_system_prompt_staging(coi_binary, workspace_dir, cleanup_containers):
    """--system-prompt-file stages a .sys file and Claude references it via
    --append-system-prompt "$(cat …)" in the printed command."""
    _write_config(workspace_dir)
    prompt_file = os.path.join(workspace_dir, "p.txt")
    with open(prompt_file, "w") as f:
        f.write("do the thing")
    sys_file = os.path.join(workspace_dir, "sys.txt")
    with open(sys_file, "w") as f:
        f.write("you are a terse assistant")

    child, container = _boot_container(coi_binary, workspace_dir)
    try:
        sid = "spec-sys-1"
        spec = _spec(
            coi_binary, container, workspace_dir, sid, prompt_file, system_prompt_file=sys_file
        )
        joined = " ".join(spec["command"])
        assert "--append-system-prompt" in joined, spec["command"]
        assert f'"$(cat /home/code/.coi/runs/{sid}.sys)"' in joined, spec["command"]
        assert "terse assistant" in _cat_in_container(
            container, f"/home/code/.coi/runs/{sid}.sys"
        )
    finally:
        _teardown(coi_binary, child, container)


def test_tool_spec_without_prompt(coi_binary, workspace_dir, cleanup_containers):
    """With no --prompt-file, the command is the bare tool launch (the
    orchestrator sends the first prompt itself), and no prompt file is staged."""
    _write_config(workspace_dir)
    child, container = _boot_container(coi_binary, workspace_dir)
    try:
        sid = "spec-noprompt-1"
        spec = _spec(coi_binary, container, workspace_dir, sid)
        assert spec["command"][0] == "dummy", spec["command"]
        # No cat-subst trailing arg since there is no prompt.
        assert not any("$(cat" in a for a in spec["command"]), spec["command"]
        # No prompt file was staged.
        absent = _cat_in_container(container, f"/home/code/.coi/runs/{sid}.prompt")
        assert "No such file" in absent or absent.strip() == "", absent
    finally:
        _teardown(coi_binary, child, container)


def test_tool_spec_codex_tool_agnostic(coi_binary, workspace_dir, cleanup_containers):
    """A different tool (codex) produces a spec through the same API with no
    caller changes — coi owns the per-tool command shape. Codex embeds the
    prompt as a trailing positional."""
    _write_config(workspace_dir, tool="codex")
    prompt_file = os.path.join(workspace_dir, "p.txt")
    with open(prompt_file, "w") as f:
        f.write("codex please")

    child, container = _boot_container(coi_binary, workspace_dir)
    try:
        sid = "spec-codex-1"
        spec = _spec(coi_binary, container, workspace_dir, sid, prompt_file)
        assert spec["command"][0] == "dummy", spec["command"]
        assert spec["command"][-1] == f'"$(cat /home/code/.coi/runs/{sid}.prompt)"', spec["command"]
        assert "codex please" in _cat_in_container(
            container, f"/home/code/.coi/runs/{sid}.prompt"
        )
    finally:
        _teardown(coi_binary, child, container)


def test_tool_spec_opencode_out_of_band_prompt(coi_binary, workspace_dir, cleanup_containers):
    """A tool that can't embed the prompt in argv (opencode) gets a `prompt`
    field with the staged in-container path, and the command has no "$(cat …)"
    prompt arg — the orchestrator delivers it out-of-band after launch."""
    _write_config(workspace_dir, tool="opencode")
    prompt_file = os.path.join(workspace_dir, "p.txt")
    with open(prompt_file, "w") as f:
        f.write("opencode please")

    child, container = _boot_container(coi_binary, workspace_dir)
    try:
        sid = "spec-opencode-1"
        spec = _spec(coi_binary, container, workspace_dir, sid, prompt_file)
        assert spec["command"][0] == "dummy", spec["command"]
        # Prompt is NOT embedded in argv for a non-embedding tool.
        assert not any("$(cat" in a for a in spec["command"]), spec["command"]
        # It's surfaced as the staged in-container prompt path instead.
        assert spec.get("prompt") == f"/home/code/.coi/runs/{sid}.prompt", spec
        assert "opencode please" in _cat_in_container(
            container, f"/home/code/.coi/runs/{sid}.prompt"
        )
    finally:
        _teardown(coi_binary, child, container)


def test_tool_spec_requires_container(coi_binary, workspace_dir):
    """`coi tool spec` without --container is a usage error (fast: the check
    short-circuits before any container access, so no boot needed)."""
    _write_config(workspace_dir)
    r = subprocess.run(
        [coi_binary, "tool", "spec", "--session-id", "x", "--json"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert r.returncode != 0, "missing --container must fail"
    assert "container" in (r.stdout + r.stderr)


def test_tool_spec_requires_session_id(coi_binary, workspace_dir):
    """`coi tool spec` without --session-id is a usage error (fast: no boot; the
    check runs before the container is touched)."""
    _write_config(workspace_dir)
    r = subprocess.run(
        [coi_binary, "tool", "spec", "--container", "nonexistent-ctr", "--json"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )
    assert r.returncode != 0, "missing --session-id must fail"
    assert "session-id" in (r.stdout + r.stderr)


def test_tool_spec_help(coi_binary):
    r = subprocess.run(
        [coi_binary, "tool", "spec", "--help"], capture_output=True, text=True, timeout=30
    )
    out = r.stdout + r.stderr
    for needle in ("--container", "--session-id", "--prompt-file", "--continue", "--json"):
        assert needle in out, f"expected {needle!r} in `coi tool spec --help`, got:\n{out}"
