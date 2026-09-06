"""
Smoke test for `coi shell --tool <name>` (#708).

Verifies the per-invocation tool override is accepted and plumbed through the
real shell launch path — the session starts and reaches a prompt. The dummy test
image installs the stub as `claude`, so the override we can exercise end-to-end
here is `--tool claude` (the switch-to-another-tool case needs a multi-tool
image); the override resolution itself is unit-tested in tool_override_test.go.
"""

import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
)


def test_shell_tool_override_starts(coi_binary, cleanup_containers, workspace_dir):
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    child = spawn_coi(
        coi_binary,
        ["shell", "--tool", "claude"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    started = False
    failure = None
    try:
        wait_for_container_ready(child, timeout=60)
        wait_for_prompt(child, timeout=90)
        started = True
    except (TimeoutError, TIMEOUT, EOF) as exc:
        failure = exc

    try:
        screen = child.logfile_read.get_display_stripped()
    except Exception:
        screen = ""

    # Cleanup
    if started:
        child.send("exit")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(1)
        child.send("sudo poweroff")
        time.sleep(0.3)
        child.send("\x0d")
    try:
        child.expect(EOF, timeout=60)
    except (TIMEOUT, EOF):
        pass
    try:
        child.close(force=False)
    except Exception:
        child.close(force=True)
    time.sleep(3)
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    assert started, f"coi shell --tool claude did not reach a prompt: {failure}\n{screen}"
