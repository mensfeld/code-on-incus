"""
Regression test for #772: `coi shell` must start under a kitty terminal.

kitty exports TERM=xterm-kitty, which has no terminfo entry in the container base
image, so before the SanitizeTerm fix tmux aborted the session with
"missing or unsuitable terminal: xterm-kitty" and the shell never came up.

This exercises the REAL end-to-end path the host-side Go unit/integration tests
can't: those run against the host's terminfo (a kitty user's machine *has*
xterm-kitty, so an unsanitized value "works" there), whereas the bug only bites
inside the container. Here we launch a real container with TERM=xterm-kitty and
assert the session reaches a prompt with no "missing or unsuitable terminal"
error — i.e. SanitizeTerm mapped it to a value the container can resolve.
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

TMUX_TERM_ERROR = "missing or unsuitable terminal"


def test_shell_starts_with_kitty_term(coi_binary, cleanup_containers, workspace_dir):
    # The dummy tool is enough — the failure is at tmux session creation, before
    # the tool runs, so no real agent auth is needed.
    env = {"COI_USE_DUMMY": "1", "TERM": "xterm-kitty"}
    container_name = calculate_container_name(workspace_dir, 1)

    child = spawn_coi(
        coi_binary,
        ["shell"],
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

    # Rendered screen for both the assertion and a useful failure message.
    try:
        screen = child.logfile_read.get_display_stripped()
    except Exception:
        screen = ""

    # === Cleanup ===
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

    # The exact symptom from #772 must not appear.
    assert TMUX_TERM_ERROR not in screen, (
        f"tmux rejected the unsanitized TERM inside the container "
        f"(TERM=xterm-kitty was not mapped to a container-resolvable value):\n{screen}"
    )
    # And the session must actually have come up.
    assert started, f"coi shell did not reach a prompt with TERM=xterm-kitty: {failure}\n{screen}"
