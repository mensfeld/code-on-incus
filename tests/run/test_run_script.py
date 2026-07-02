"""
Tests for the workspace run-script convention (coi-run).

`coi run` with no command detects an executable `coi-run` at the workspace
root and executes it inside the container straight from the workspace mount
(nothing is pushed): output streams, the script's exit code becomes coi's exit
code, and the ephemeral container is cleaned up afterwards. The file is
extensionless and executed directly, so the shebang decides the interpreter
(bash, ruby, python, ...); a non-executable coi-run is a fail-fast error with
a chmod hint rather than an interpreter guess.
"""

import os
import subprocess

from support.helpers import write_workspace_container_config


def _run_coi(coi_binary, workspace_dir, timeout=180):
    return subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=timeout,
    )


def _write_script(workspace_dir, content):
    script = os.path.join(workspace_dir, "coi-run")
    with open(script, "w") as f:
        f.write(content)
    os.chmod(script, 0o755)
    return script


def test_run_script_runs_and_writes_to_workspace(coi_binary, cleanup_containers, workspace_dir):
    """An executable coi-run runs in the sandbox; its workspace writes persist
    to the host (the script operates on the real mounted workspace)."""
    # Make the workspace writable by the container's code user through the
    # shift mapping (the pytest tmp dir is owned by the CI runner's uid).
    os.chmod(workspace_dir, 0o777)
    _write_script(
        workspace_dir,
        "#!/usr/bin/env bash\nset -e\necho run-script-running\necho ok > run-sentinel.txt\n",
    )

    result = _run_coi(coi_binary, workspace_dir)

    combined = result.stdout + result.stderr
    assert result.returncode == 0, f"run script should succeed. Output:\n{combined}"
    assert "run-script-running" in combined, f"script output should stream. Got:\n{combined}"
    assert "coi-run" in combined, f"coi should announce the run script. Got:\n{combined}"

    sentinel = os.path.join(workspace_dir, "run-sentinel.txt")
    assert os.path.exists(sentinel), "script's workspace write must persist to the host"
    with open(sentinel) as f:
        assert f.read().strip() == "ok"


def test_run_script_shebang_decides_interpreter(coi_binary, cleanup_containers, workspace_dir):
    """coi-run is executed directly, so a non-bash shebang works too (the
    default image ships python3)."""
    _write_script(
        workspace_dir,
        "#!/usr/bin/env python3\nprint('python-shebang-works')\n",
    )

    result = _run_coi(coi_binary, workspace_dir)

    combined = result.stdout + result.stderr
    assert result.returncode == 0, f"python coi-run should succeed. Got:\n{combined}"
    assert "python-shebang-works" in combined, f"python output expected. Got:\n{combined}"


def test_run_script_exit_code_propagates(coi_binary, cleanup_containers, workspace_dir):
    """The script's exit code becomes coi run's exit code."""
    _write_script(workspace_dir, "#!/usr/bin/env bash\necho about-to-fail\nexit 3\n")

    result = _run_coi(coi_binary, workspace_dir)

    assert result.returncode == 3, (
        f"want exit code 3 from the script, got {result.returncode}. "
        f"Output:\n{result.stdout + result.stderr}"
    )


def test_run_script_non_executable_fails_with_chmod_hint(
    coi_binary, cleanup_containers, workspace_dir
):
    """A non-executable coi-run is a fail-fast error with a chmod hint —
    no interpreter guessing, and no container is launched."""
    script = os.path.join(workspace_dir, "coi-run")
    with open(script, "w") as f:
        f.write("echo should-not-run\n")
    os.chmod(script, 0o644)

    result = _run_coi(coi_binary, workspace_dir, timeout=30)

    combined = result.stdout + result.stderr
    assert result.returncode != 0, f"non-executable coi-run must fail. Got:\n{combined}"
    assert "chmod +x" in combined, f"error should carry the chmod hint. Got:\n{combined}"
    assert "should-not-run" not in combined, "the script must not have been executed"


def test_run_script_persistent_state_survives(coi_binary, cleanup_containers, workspace_dir):
    """With [container] persistent = true, container state outside the
    workspace (home dir) survives between run-script runs."""
    write_workspace_container_config(workspace_dir, persistent=True)

    _write_script(
        workspace_dir,
        "#!/usr/bin/env bash\n"
        "if [ -f ~/state-marker ]; then echo SECOND-RUN-SEES-STATE; fi\n"
        "touch ~/state-marker\n",
    )

    # No slot pinning on purpose: persistent mode must reuse the stopped
    # container from the first run automatically (FindReusablePersistentSlot)
    # rather than drifting to a fresh slot and silently losing the state.
    first = _run_coi(coi_binary, workspace_dir)
    assert first.returncode == 0, f"first run failed:\n{first.stdout + first.stderr}"
    assert "SECOND-RUN-SEES-STATE" not in first.stdout + first.stderr

    second = _run_coi(coi_binary, workspace_dir)
    combined = second.stdout + second.stderr
    assert second.returncode == 0, f"second run failed:\n{combined}"
    assert "SECOND-RUN-SEES-STATE" in combined, (
        f"persistent runs must reuse the stopped container (no --slot needed). Got:\n{combined}"
    )
    assert "Reusing persistent container" in combined, (
        f"second run should announce slot reuse. Got:\n{combined}"
    )
