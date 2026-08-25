"""
Test that [tool] context_json = false disables ~/SANDBOX_CONTEXT.json (#705).

Verifies that when context_json = false:
1. ~/SANDBOX_CONTEXT.json is NOT created in the container.
2. ~/SANDBOX_CONTEXT.md is still created (the toggle is JSON-only).
"""

import os
import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
)


def _file_status(container_name, path):
    result = subprocess.run(
        [
            "sg",
            "incus-admin",
            "-c",
            f"incus exec {container_name} -- test -f {path} && echo exists || echo missing",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    return result.stdout.strip()


def test_context_json_opt_out(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    """context_json = false skips SANDBOX_CONTEXT.json but keeps SANDBOX_CONTEXT.md."""
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    config_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(config_dir, exist_ok=True)
    with open(os.path.join(config_dir, "config.toml"), "w") as f:
        f.write("[tool]\ncontext_json = false\n")

    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    claude_dir = fake_home / ".claude"
    claude_dir.mkdir()
    (claude_dir / ".credentials.json").write_text('{"token": "test"}')
    env["HOME"] = str(fake_home)

    child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir, env=env, timeout=120)
    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    json_status = _file_status(container_name, "/home/code/SANDBOX_CONTEXT.json")
    md_status = _file_status(container_name, "/home/code/SANDBOX_CONTEXT.md")

    # Cleanup
    child.send("sudo poweroff")
    time.sleep(0.3)
    child.send("\x0d")
    try:
        child.expect(EOF, timeout=60)
    except TIMEOUT:
        pass
    try:
        child.close(force=False)
    except Exception:
        child.close(force=True)
    time.sleep(5)
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    assert json_status == "missing", (
        f"~/SANDBOX_CONTEXT.json should NOT exist when context_json=false, got {json_status!r}"
    )
    assert md_status == "exists", (
        f"~/SANDBOX_CONTEXT.md should still exist (toggle is JSON-only), got {md_status!r}"
    )
