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

This test:

1. Writes `.coi/config.toml` with a unique custom image alias and a
   minimal `[container.build]` section that defaults to coi-default
   as its base.
2. Runs `coi build` to materialize the custom image.
3. Runs `coi shell --debug --background` — the debug flag replaces the
   AI tool with plain bash, and --background creates a detached tmux
   session and returns without needing a TTY.
4. Sends `whoami > /tmp/coi_user_probe` to the tmux session.
5. Reads the probe file via `coi container exec`.
6. Asserts the output contains `code` and not `root`. On master before
   the fix this assertion fails — the probe file contains `root`.
7. Cleans up the container and the image alias.
"""

import os
import subprocess
import time
from pathlib import Path

from support.helpers import calculate_container_name


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
        return

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

        # === Phase 3: start a detached tmux shell against the custom image ===
        #
        # `coi shell --debug --background`:
        #   - --debug runs bash instead of the configured AI tool
        #   - --background creates a detached tmux session and returns
        #     (no TTY required, safe for subprocess.run)
        #   - COI_USE_DUMMY=1 is harmless here but keeps parity with the
        #     rest of the shell test suite
        #
        # This is the exact code path that exhibits the bug: shell.go ->
        # session.Setup -> RunAsRoot decision -> buildContainerEnv ->
        # ExecCommand(tmux new-session, User=userPtr). With the bug
        # present, userPtr == 0 (root) for any non-"coi-default" image.
        env = {**os.environ, "COI_USE_DUMMY": "1"}
        result = subprocess.run(
            [coi_binary, "shell", "--debug", "--background"],
            capture_output=True,
            text=True,
            timeout=180,
            cwd=workspace_dir,
            env=env,
        )
        assert result.returncode == 0, (
            f"coi shell --debug --background should succeed. "
            f"stdout: {result.stdout}\nstderr: {result.stderr}"
        )

        # === Phase 4: probe the tmux session's user ===
        #
        # Give tmux a moment to finish initializing its prompt before
        # sending keys — otherwise the send-keys can race the bash
        # startup and the command gets swallowed.
        time.sleep(2)

        marker = "COI_WHOAMI_MARKER"
        # Send a shell command to the detached tmux session that writes
        # `whoami` output plus a marker to a probe file. The marker lets
        # us distinguish "bug present / probe really ran" from "probe
        # never ran and the file is whatever was there before."
        probe_cmd = f'whoami > /tmp/coi_user_probe && echo "{marker}" >> /tmp/coi_user_probe'
        result = subprocess.run(
            [coi_binary, "tmux", "send", container_name, probe_cmd],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert result.returncode == 0, f"coi tmux send should succeed. stderr: {result.stderr}"

        # Give the in-tmux command a moment to actually execute.
        time.sleep(2)

        # Read the probe file via `coi container exec` (reads as root so
        # it can read a probe file owned by anyone).
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "cat",
                "/tmp/coi_user_probe",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert result.returncode == 0, f"Reading probe file should succeed. stderr: {result.stderr}"

        # `coi container exec` may route the command's stdout through
        # its own stderr when no TTY is attached — check both streams.
        combined = result.stdout + result.stderr

        assert marker in combined, (
            f"Probe command never ran in the tmux session — marker not "
            f"found in probe file. Output:\n--- stdout ---\n{result.stdout}\n"
            f"--- stderr ---\n{result.stderr}"
        )

        # The CRITICAL assertions. Before the fix, the probe file contains
        # "root". After the fix, it contains "code".
        assert "code" in combined.splitlines() or "code\n" in combined, (
            f"Custom image inheriting from coi-default must run `coi shell` "
            f"sessions as `code`, not root. Probe file output:\n"
            f"--- stdout ---\n{result.stdout}\n--- stderr ---\n{result.stderr}"
        )
        assert "root" not in combined.splitlines() and "root\n" not in combined, (
            f"Custom image inheriting from coi-default must NOT run `coi shell` "
            f"sessions as root. Probe file output:\n"
            f"--- stdout ---\n{result.stdout}\n--- stderr ---\n{result.stderr}"
        )

    finally:
        # === Phase 5: cleanup ===
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
