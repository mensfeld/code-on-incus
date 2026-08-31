"""
Headless, tool-agnostic launch: `coi shell --headless` (#746).

An external orchestrator prepares + drives a tool run without re-implementing
the tool's CLI: coi builds the command from the profile's tool (session id,
resume, model, permission, env) and the caller supplies only the dynamics
(prompt, resume mode). These assert the end-to-end contract with the dummy tool:

1. --headless --json prints machine-readable handles (container, tmux session,
   session id, exit file).
2. The prompt is staged into the container (delivered to the tool), not lost.
3. `coi tmux status --session-id` reports running, then done+exit-code once the
   run finishes (driven here by sending `exit` to the dummy).
"""

import json
import os
import subprocess
from pathlib import Path

import pytest


def _headless_env():
    return {**os.environ, "COI_USE_DUMMY": "1"}


def _write_profile(workspace_dir, name, tool="claude", model=None):
    d = Path(workspace_dir) / ".coi" / "profiles" / name
    d.mkdir(parents=True, exist_ok=True)
    body = f"""
[container]
image = "coi-default"
persistent = true

[tool]
name = "{tool}"
"""
    if model:
        body += f'\n[tool.{tool}]\nmodel = "{model}"\n'
    (d / "config.toml").write_text(body)


def _launch_headless(
    coi_binary, workspace_dir, profile, prompt_file=None, system_prompt_file=None, extra=None
):
    argv = [
        coi_binary,
        "shell",
        "--workspace",
        workspace_dir,
        "--profile",
        profile,
        "--headless",
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
        timeout=120,
        env=_headless_env(),
        cwd=workspace_dir,
    )
    assert r.returncode == 0, f"headless launch failed. stdout:{r.stdout}\nstderr:{r.stderr}"
    return _last_json(r.stdout)


def _last_json(stdout):
    """Return the last stdout line that parses as a JSON object (handles must be
    machine-readable even if a stray line lands on stdout)."""
    for ln in reversed(stdout.splitlines()):
        ln = ln.strip()
        if ln.startswith("{"):
            try:
                return json.loads(ln)
            except json.JSONDecodeError:
                continue
    raise AssertionError(f"no JSON object on stdout:\n{stdout}")


def _cat_in_container(container, path):
    r = subprocess.run(
        ["incus", "exec", container, "--", "cat", path],
        capture_output=True,
        text=True,
        timeout=20,
    )
    return r.stdout + r.stderr


def _status(coi_binary, container, sid, wait=False, timeout=None):
    argv = [coi_binary, "tmux", "status", container, "--session-id", sid, "--json"]
    if wait:
        argv += ["--wait"]
    if timeout is not None:
        argv += ["--timeout", str(timeout)]
    r = subprocess.run(argv, capture_output=True, text=True, timeout=(timeout or 20) + 30)
    return r


def _drive_to_completion(coi_binary, container, sid):
    """Send `exit` to the dummy so the launch script records the exit sentinel,
    then wait for done and return the parsed final status."""
    subprocess.run(
        [coi_binary, "tmux", "send", container, "exit"],
        capture_output=True,
        text=True,
        timeout=20,
    )
    done = _status(coi_binary, container, sid, wait=True, timeout=30)
    assert done.returncode == 0, f"wait status failed (exit {done.returncode}): {done.stderr}"
    return _last_json(done.stdout)


def _delete(coi_binary, container):
    subprocess.run(
        [coi_binary, "container", "delete", container, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_headless_launch_and_completion(coi_binary, workspace_dir, cleanup_containers):
    """Core flow: launch -> handles -> prompt staged -> running -> done+exit0."""
    _write_profile(workspace_dir, "headless")
    prompt_file = Path(workspace_dir) / "prompt.txt"
    prompt_text = "hello from the orchestrator"
    prompt_file.write_text(prompt_text)

    handles = _launch_headless(coi_binary, workspace_dir, "headless", prompt_file)
    for key in ("container", "tmux_session", "session_id", "exit_file"):
        assert handles.get(key), f"missing handle {key}: {handles}"
    container = handles["container"]
    sid = handles["session_id"]
    assert handles["tmux_session"] == f"coi-{container}"
    assert handles["exit_file"] == f"/home/code/.coi/runs/{sid}.exit"

    try:
        # (2) The prompt was staged into the container for the tool to read.
        assert prompt_text in _cat_in_container(container, f"/home/code/.coi/runs/{sid}.prompt")

        # (3a) Before completion, status reports the run as still running.
        st = _status(coi_binary, container, sid)
        assert st.returncode == 0, f"status failed: {st.stderr}"
        running = _last_json(st.stdout)
        assert running["state"] == "running", f"expected running, got {running}"

        # (3b) After the tool exits, status reports done with the tool's exit code.
        final = _drive_to_completion(coi_binary, container, sid)
        assert final["state"] == "done", f"expected done, got {final}"
        assert final["exit_code"] == 0, f"expected exit 0, got {final}"
    finally:
        _delete(coi_binary, container)


def test_headless_special_char_prompt_staged_verbatim(
    coi_binary, workspace_dir, cleanup_containers
):
    """A prompt with quotes/newlines/$() reaches the container verbatim — the
    whole point of staging it as a file rather than embedding it in the command
    (which passes through several shell layers)."""
    _write_profile(workspace_dir, "headless")
    prompt_file = Path(workspace_dir) / "tricky.txt"
    prompt_text = "line1 'single' \"double\" $(whoami) `id` && echo pwned\nline2\n"
    prompt_file.write_text(prompt_text)

    handles = _launch_headless(coi_binary, workspace_dir, "headless", prompt_file)
    container, sid = handles["container"], handles["session_id"]
    try:
        staged = _cat_in_container(container, f"/home/code/.coi/runs/{sid}.prompt")
        # Verbatim: no command substitution ran, both quote styles survived.
        assert "$(whoami)" in staged, f"substitution must not have executed: {staged!r}"
        assert "'single'" in staged and '"double"' in staged
        assert "pwned" in staged and "line2" in staged
        # The container still comes up healthy and completes.
        final = _drive_to_completion(coi_binary, container, sid)
        assert final["state"] == "done"
    finally:
        _delete(coi_binary, container)


def test_headless_system_prompt_staged(coi_binary, workspace_dir, cleanup_containers):
    """--system-prompt-file is staged to a .sys file for Claude's system prompt."""
    _write_profile(workspace_dir, "headless")
    prompt_file = Path(workspace_dir) / "p.txt"
    prompt_file.write_text("do the thing")
    sys_file = Path(workspace_dir) / "sys.txt"
    sys_file.write_text("you are a terse assistant")

    handles = _launch_headless(
        coi_binary, workspace_dir, "headless", prompt_file, system_prompt_file=sys_file
    )
    container, sid = handles["container"], handles["session_id"]
    try:
        assert "terse assistant" in _cat_in_container(container, f"/home/code/.coi/runs/{sid}.sys")
        # The launch script references both files via cat-substitution.
        script = _cat_in_container(container, f"/home/code/.coi/runs/{sid}.sh")
        assert f"{sid}.prompt" in script and f"{sid}.sys" in script
        assert "--append-system-prompt" in script
    finally:
        _delete(coi_binary, container)


def test_headless_applies_profile_model(coi_binary, workspace_dir, cleanup_containers):
    """The profile's [tool.claude] model reaches the headless run as container
    env (ties #744 into the headless path)."""
    _write_profile(workspace_dir, "modeled", model="coi-test-headless-opus")
    prompt_file = Path(workspace_dir) / "p.txt"
    prompt_file.write_text("hi")

    handles = _launch_headless(coi_binary, workspace_dir, "modeled", prompt_file)
    container = handles["container"]
    try:
        env = subprocess.run(
            ["incus", "config", "get", container, "environment.ANTHROPIC_MODEL"],
            capture_output=True,
            text=True,
            timeout=15,
        )
        assert env.stdout.strip() == "coi-test-headless-opus", (
            f"headless must apply the profile model as container env, got {env.stdout!r}"
        )
    finally:
        _delete(coi_binary, container)


def test_headless_launch_without_prompt(coi_binary, workspace_dir, cleanup_containers):
    """--headless with no --prompt-file still launches and returns handles (the
    orchestrator may send the first prompt itself)."""
    _write_profile(workspace_dir, "noprompt")
    handles = _launch_headless(coi_binary, workspace_dir, "noprompt")
    container, sid = handles["container"], handles["session_id"]
    try:
        # No prompt file was staged.
        absent = _cat_in_container(container, f"/home/code/.coi/runs/{sid}.prompt")
        assert "No such file" in absent or absent.strip() == "", (
            f"no prompt file should exist, got {absent!r}"
        )
        # The script still runs the tool and records completion on exit.
        final = _drive_to_completion(coi_binary, container, sid)
        assert final["state"] == "done"
    finally:
        _delete(coi_binary, container)


def test_headless_codex_tool_agnostic(coi_binary, workspace_dir, cleanup_containers):
    """A different tool (codex) drives through the same headless API with no
    orchestrator changes — coi owns the per-tool command shape."""
    _write_profile(workspace_dir, "cx", tool="codex")
    prompt_file = Path(workspace_dir) / "p.txt"
    prompt_file.write_text("codex please")

    handles = _launch_headless(coi_binary, workspace_dir, "cx", prompt_file)
    container, sid = handles["container"], handles["session_id"]
    try:
        assert "codex please" in _cat_in_container(container, f"/home/code/.coi/runs/{sid}.prompt")
        final = _drive_to_completion(coi_binary, container, sid)
        assert final["state"] == "done"
    finally:
        _delete(coi_binary, container)


def test_tmux_status_requires_session_id(coi_binary, workspace_dir, cleanup_containers):
    """`coi tmux status` without --session-id is a usage error (fast, no launch)."""
    _write_profile(workspace_dir, "headless")
    handles = _launch_headless(coi_binary, workspace_dir, "headless")
    container = handles["container"]
    try:
        r = subprocess.run(
            [coi_binary, "tmux", "status", container],
            capture_output=True,
            text=True,
            timeout=20,
        )
        assert r.returncode != 0, "missing --session-id must fail"
        assert "session-id" in (r.stdout + r.stderr)
    finally:
        _delete(coi_binary, container)


@pytest.mark.parametrize(
    "argv,needles",
    [
        (["shell", "--help"], ["--headless", "--prompt-file", "--system-prompt-file"]),
        (["tmux", "status", "--help"], ["--session-id", "--wait", "--json"]),
    ],
)
def test_headless_help(coi_binary, argv, needles):
    r = subprocess.run([coi_binary, *argv], capture_output=True, text=True, timeout=30)
    out = r.stdout + r.stderr
    for n in needles:
        assert n in out, f"expected {n!r} in `coi {' '.join(argv)}`, got:\n{out}"
