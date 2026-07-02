"""
Tests for the workspace boot-script convention (coi-boot.sh).

`coi run` with no command detects coi-boot.sh at the workspace root and
executes it inside the container straight from the workspace mount (nothing is
pushed): output streams, the script's exit code becomes coi's exit code, and
the ephemeral container is cleaned up afterwards. An executable script runs
directly (shebang respected); a non-executable one runs via bash.
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


def test_boot_script_runs_and_writes_to_workspace(coi_binary, cleanup_containers, workspace_dir):
    """An executable coi-boot.sh runs in the sandbox; its workspace writes
    persist to the host (the script operates on the real mounted workspace)."""
    script = os.path.join(workspace_dir, "coi-boot.sh")
    with open(script, "w") as f:
        f.write(
            "#!/usr/bin/env bash\nset -e\necho boot-script-running\necho ok > boot-sentinel.txt\n"
        )
    os.chmod(script, 0o755)

    result = _run_coi(coi_binary, workspace_dir)

    combined = result.stdout + result.stderr
    assert result.returncode == 0, f"boot script should succeed. Output:\n{combined}"
    assert "boot-script-running" in combined, f"script output should stream. Got:\n{combined}"
    assert "coi-boot.sh" in combined, f"coi should announce the boot script. Got:\n{combined}"

    sentinel = os.path.join(workspace_dir, "boot-sentinel.txt")
    assert os.path.exists(sentinel), "script's workspace write must persist to the host"
    with open(sentinel) as f:
        assert f.read().strip() == "ok"


def test_boot_script_exit_code_propagates(coi_binary, cleanup_containers, workspace_dir):
    """The script's exit code becomes coi run's exit code."""
    script = os.path.join(workspace_dir, "coi-boot.sh")
    with open(script, "w") as f:
        f.write("#!/usr/bin/env bash\necho about-to-fail\nexit 3\n")
    os.chmod(script, 0o755)

    result = _run_coi(coi_binary, workspace_dir)

    assert result.returncode == 3, (
        f"want exit code 3 from the script, got {result.returncode}. "
        f"Output:\n{result.stdout + result.stderr}"
    )


def test_boot_script_non_executable_runs_via_bash(coi_binary, cleanup_containers, workspace_dir):
    """A coi-boot.sh without the executable bit still runs (bash fallback)."""
    script = os.path.join(workspace_dir, "coi-boot.sh")
    with open(script, "w") as f:
        f.write("echo non-exec-fallback-works\n")
    os.chmod(script, 0o644)

    result = _run_coi(coi_binary, workspace_dir)

    combined = result.stdout + result.stderr
    assert result.returncode == 0, f"non-executable script should run via bash. Got:\n{combined}"
    assert "non-exec-fallback-works" in combined, f"script output expected. Got:\n{combined}"


def test_boot_script_persistent_state_survives(coi_binary, cleanup_containers, workspace_dir):
    """With [container] persistent = true, container state outside the
    workspace (home dir) survives between boot-script runs."""
    write_workspace_container_config(workspace_dir, persistent=True)

    script = os.path.join(workspace_dir, "coi-boot.sh")
    with open(script, "w") as f:
        f.write(
            "#!/usr/bin/env bash\n"
            "if [ -f ~/state-marker ]; then echo SECOND-RUN-SEES-STATE; fi\n"
            "touch ~/state-marker\n"
        )
    os.chmod(script, 0o755)

    first = _run_coi(coi_binary, workspace_dir)
    assert first.returncode == 0, f"first run failed:\n{first.stdout + first.stderr}"
    assert "SECOND-RUN-SEES-STATE" not in first.stdout + first.stderr

    second = _run_coi(coi_binary, workspace_dir)
    combined = second.stdout + second.stderr
    assert second.returncode == 0, f"second run failed:\n{combined}"
    assert "SECOND-RUN-SEES-STATE" in combined, (
        f"persistent container should preserve home-dir state between runs. Got:\n{combined}"
    )
