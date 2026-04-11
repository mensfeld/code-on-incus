"""
Regression test: a custom image built from coi-default must launch
`coi shell` sessions as the `code` user, not as root.

Bug report: a user with `[container] image = "coi-..."` and a
`[container.build]` profile (default base = coi-default) ran `coi shell`
and landed in a root prompt, while `coi attach` to the same container
correctly showed the `code` user. Tracing the discrepancy:

  internal/session/setup.go:163
    usingCoiImage := image == CoiImage    // literal "coi-default"
    result.RunAsRoot = !usingCoiImage

The check is a string match against the alias, not a real probe of the
image. A custom image built FROM coi-default has the `code` user
inherited from the base layer, but its alias is something like
`coi-rust-dev`, so the literal match returns false and the session is
forced to root. `coi attach` hardcodes `container.CodeUID` in
internal/cli/attach.go:132,173 and is not affected. `coi run` also has
its own inline orchestration with `--user container.CodeUID` hardcoded
in internal/cli/run.go:262, so it also bypasses the bug. Only `coi
shell` — which goes through session.SetupSession → RunAsRoot →
buildContainerEnv:520 — exhibits the issue.

This test uses the same pexpect-driven pattern as the rest of the
shell/ephemeral suite (see e.g. uid_mapping_correct_ephemeral.py):

1. Writes `.coi/config.toml` with a unique custom image alias and a
   minimal `[container.build]` section that defaults to coi-default
   as its base.
2. Runs `coi build` to materialize the custom image.
3. Spawns `coi shell` interactively with COI_USE_DUMMY=1 so the inner
   CLI is a scripted dummy.
4. Waits for the container to come up and the dummy prompt to appear.
5. Exits the dummy CLI to drop to the inner bash shell.
6. Sends a marker-wrapped `whoami` (`echo USER_$(whoami)_PROBE`) and
   asserts that `USER_code_PROBE` appears on the terminal screen and
   `USER_root_PROBE` does not. On master before the fix this assertion
   fails — the bash in the tmux session is running as root, so the
   probe prints `USER_root_PROBE`.
7. Powers off and cleans up the container and image alias.
"""

import subprocess
import time
from pathlib import Path

import pytest
from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)


def test_custom_image_from_coi_default_shell_runs_as_code(
    coi_binary, workspace_dir, cleanup_containers
):
    """A custom image whose base is coi-default must still run `coi shell` as `code`."""
    image_name = "coi-test-custom-shell-runs-as-code"
    container_name = calculate_container_name(workspace_dir, 1)

    # Skip if the base image isn't built — this test cannot run without it.
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        pytest.skip("coi-default image not built; cannot exercise custom-from-default path")

    # Best-effort cleanup of any leftover image from a prior failed run.
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    try:
        # === Phase 1: minimal custom-build profile config ===
        #
        # No `base = ...` line in [container.build] — this exercises the
        # exact path the bug reporter hit, and proves the default base
        # really is coi-default. The build command is a trivial marker so
        # the build is fast.
        config_dir = Path(workspace_dir) / ".coi"
        config_dir.mkdir(exist_ok=True)
        (config_dir / "config.toml").write_text(
            f"""
[container]
image = "{image_name}"

[container.build]
commands = ["echo custom-image-marker > /tmp/custom_image_marker"]
"""
        )

        # === Phase 2: build the custom image (base = coi-default) ===
        result = subprocess.run(
            [coi_binary, "build"],
            capture_output=True,
            text=True,
            timeout=600,
            cwd=workspace_dir,
        )
        assert result.returncode == 0, f"Custom build should succeed. stderr: {result.stderr}"

        # Sanity: the custom image now exists with the configured alias.
        result = subprocess.run(
            [coi_binary, "image", "exists", image_name],
            capture_output=True,
        )
        assert result.returncode == 0, f"Custom image '{image_name}' should exist after build"

        # === Phase 3: spawn coi shell interactively ===
        #
        # This is the exact code path that exhibits the bug: shell.go ->
        # session.Setup -> RunAsRoot decision -> buildContainerEnv ->
        # ExecCommand(tmux new-session, User=userPtr). With the bug
        # present, userPtr == 0 (root) for any non-"coi-default" image.
        env = {"COI_USE_DUMMY": "1"}
        child = spawn_coi(
            coi_binary,
            ["shell"],
            cwd=workspace_dir,
            env=env,
            timeout=180,
        )

        wait_for_container_ready(child, timeout=90)
        wait_for_prompt(child, timeout=120)

        # === Phase 4: drop from dummy CLI to bash and probe whoami ===
        #
        # The dummy CLI accepts `exit` to terminate; tmux's trap+exec-bash
        # wrapper then lands us in a bash shell running as the session
        # user. From there, `echo USER_$(whoami)_PROBE` gives us a
        # marker-wrapped user name that cannot be mistaken for any other
        # text on screen (e.g. the word "code" appearing in "opencode").
        child.send("exit")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(3)

        marker_found_code = False
        marker_found_root = False
        with with_live_screen(child) as monitor:
            time.sleep(1)
            child.send("echo USER_$(whoami)_PROBE")
            time.sleep(0.3)
            child.send("\x0d")
            marker_found_code = wait_for_text_in_monitor(monitor, "USER_code_PROBE", timeout=20)
            # Only bother to check for root if the code marker was not
            # found — wait_for_text_in_monitor on a miss just burns the
            # full timeout, and we only need one or the other to decide.
            if not marker_found_code:
                display = monitor.last_display if hasattr(monitor, "last_display") else ""
                marker_found_root = "USER_root_PROBE" in (display or "")

        assert marker_found_code, (
            "Custom image inheriting from coi-default must run `coi shell` "
            "sessions as `code`, not root. `whoami` probe did not show "
            "`USER_code_PROBE` on screen."
            + (" Probe showed `USER_root_PROBE` instead." if marker_found_root else "")
        )

        # === Phase 5: clean shutdown ===
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

    finally:
        # === Phase 6: cleanup ===
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            check=False,
            capture_output=True,
            timeout=30,
        )
        subprocess.run(
            [coi_binary, "image", "delete", image_name],
            check=False,
            capture_output=True,
        )
