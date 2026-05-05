"""
Test that forwarded environment variables are NOT leaked via ps output.

Regression test for the fix in PR #352: environment variables forwarded into
tmux sessions must be passed via `tmux new-session -e KEY=VAL` flags (which
are not visible in process listings) rather than inlined as
`export KEY=VAL; ...` in the shell command string (which IS visible in
`ps auxww`).

Tests that:
1. Launch coi shell --background with forward_env containing a secret
2. Verify ps auxww inside the container does NOT expose the secret value
3. Verify the secret IS available inside the tmux session (functional correctness)
"""

import json
import os
import subprocess
import time
from pathlib import Path

from support.helpers import calculate_container_name


def _get_container_state(name):
    """Get container state via incus."""
    result = subprocess.run(
        ["incus", "list", name, "--format=json"],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        return "Unknown"
    containers = json.loads(result.stdout)
    return containers[0].get("status", "Unknown") if containers else "Unknown"


def _wait_for_container_running(name, timeout=60):
    """Wait for container to reach Running state."""
    for _ in range(timeout):
        if _get_container_state(name) == "Running":
            return True
        time.sleep(1)
    return False


def test_forwarded_env_not_visible_in_ps(coi_binary, cleanup_containers, workspace_dir):
    """
    Forwarded env vars must NOT appear in `ps auxww` output.

    The old implementation inlined `export KEY=VAL; ...` into the tmux command
    string, which made secrets (GITHUB_TOKEN, etc.) visible to any process in
    the container that runs `ps auxww`.

    The fix uses `tmux new-session -e KEY=VAL` which keeps values out of the
    process table.

    Flow:
    1. Create .coi/config.toml with forward_env = ["COI_SECRET_TOKEN"]
    2. Set COI_SECRET_TOKEN to a recognisable marker value on the host
    3. Launch coi shell --background (creates a detached tmux session)
    4. Wait for the container to be running and tmux session to exist
    5. Run `ps auxww` inside the container
    6. Assert the marker value does NOT appear in ps output
    7. Assert the variable IS accessible inside the tmux pane (functional check)
    """
    container_name = calculate_container_name(workspace_dir, 1)
    secret_value = "SUPER_SECRET_TOKEN_ps_leak_test_7x9k2m"

    # === Phase 1: Create config with forward_env ===

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
forward_env = ["COI_SECRET_TOKEN"]
"""
    )

    # === Phase 2: Launch shell in background with the secret set ===

    env = os.environ.copy()
    env["COI_USE_DUMMY"] = "1"
    env["COI_SECRET_TOKEN"] = secret_value

    proc = subprocess.Popen(
        [
            coi_binary,
            "shell",
            "--background",
            "--workspace",
            workspace_dir,
            "--slot",
            "1",
        ],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
    )

    # Wait for coi shell --background to finish (it exits after creating the session)
    try:
        stdout, stderr = proc.communicate(timeout=120)
    except subprocess.TimeoutExpired:
        proc.kill()
        stdout, stderr = proc.communicate()
        raise AssertionError(f"coi shell --background timed out. stderr: {stderr.decode()}")

    assert proc.returncode == 0, (
        f"coi shell --background should succeed. "
        f"stdout: {stdout.decode()}\nstderr: {stderr.decode()}"
    )

    # === Phase 3: Wait for container to be running ===

    assert _wait_for_container_running(container_name, timeout=60), (
        f"Container {container_name} never reached Running state"
    )

    # Give tmux session a moment to fully initialise
    time.sleep(3)

    # === Phase 4: Check ps auxww does NOT contain the secret ===

    result = subprocess.run(
        [coi_binary, "container", "exec", "--capture", container_name, "--", "ps", "auxww"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"ps auxww should succeed. stderr: {result.stderr}"

    ps_output = result.stdout + result.stderr
    assert secret_value not in ps_output, (
        f"SECRET VALUE LEAKED IN PS OUTPUT! "
        f"The secret '{secret_value}' must NOT appear in `ps auxww`. "
        f"This means environment variables are being inlined in the command string "
        f"rather than passed via tmux -e flags.\n\nps output:\n{ps_output}"
    )

    # === Phase 5: Verify the variable IS available inside the tmux pane ===
    # The tmux pane starts with the dummy CLI. Exit it to get bash
    # (session cmd: bash -c 'trap : INT; dummy ...; exec bash')

    tmux_session = f"coi-{container_name}"

    # Exit the dummy CLI to drop into bash
    subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--user",
            "1000",
            "--",
            "tmux",
            "send-keys",
            "-t",
            tmux_session,
            "exit",
            "Enter",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    time.sleep(3)

    # Now send echo command in bash
    subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--user",
            "1000",
            "--",
            "tmux",
            "send-keys",
            "-t",
            tmux_session,
            "echo ENV_CHECK_${COI_SECRET_TOKEN}_END",
            "Enter",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    time.sleep(2)

    # Capture pane output
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            "--capture",
            container_name,
            "--user",
            "1000",
            "--",
            "tmux",
            "capture-pane",
            "-t",
            tmux_session,
            "-p",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"tmux capture-pane should succeed. stderr: {result.stderr}"

    pane_output = result.stdout + result.stderr
    assert f"ENV_CHECK_{secret_value}_END" in pane_output, (
        f"Forwarded env var should be accessible inside the tmux session. "
        f"Expected 'ENV_CHECK_{secret_value}_END' in pane output.\n"
        f"Got:\n{pane_output}"
    )

    # === Phase 6: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
