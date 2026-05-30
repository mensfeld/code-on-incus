"""
Test for [shell] use_tmux config option in coi shell.

Tests that:
1. use_tmux = false in config causes shell to run in direct mode (no tmux)
2. --tmux=true flag overrides use_tmux = false config
3. use_tmux = true (default) causes shell to use tmux mode
"""

import subprocess
from pathlib import Path


def test_use_tmux_false_config_runs_direct_mode(coi_binary, workspace_dir, cleanup_containers):
    """
    When use_tmux = false in [shell] config, coi shell should run in Direct mode.

    --background is a tmux concept and is not used here. Instead, we run in
    direct mode and feed "exit" to bash via stdin so the session terminates.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("""
[shell]
use_tmux = false
""")

    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--debug"],
        input="exit\n",
        capture_output=True,
        text=True,
        timeout=120,
    )

    combined = result.stdout + result.stderr
    assert "Mode: Direct" in combined, (
        f"Expected 'Mode: Direct' with use_tmux=false config. Output:\n{combined}"
    )
    assert (
        "Mode: Interactive (tmux)" not in combined and "Mode: Background (tmux)" not in combined
    ), f"Expected no tmux mode with use_tmux=false config. Output:\n{combined}"


def test_tmux_flag_overrides_config(coi_binary, workspace_dir, cleanup_containers):
    """
    --tmux=true flag should override use_tmux = false in config.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("""
[shell]
use_tmux = false
""")

    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--background",
            "--tmux=true",
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )

    combined = result.stdout + result.stderr
    # --tmux=true explicitly overrides use_tmux=false; must show a tmux Mode line
    assert "Mode: Background (tmux)" in combined or "Mode: Interactive (tmux)" in combined, (
        f"Expected tmux Mode line when --tmux=true overrides use_tmux=false config. Output:\n{combined}"
    )
    assert "Mode: Direct" not in combined, (
        f"Expected --tmux=true to override use_tmux=false config. Output:\n{combined}"
    )


def test_use_tmux_true_config_default(coi_binary, workspace_dir, cleanup_containers):
    """
    use_tmux = true (explicit or default) should behave as before — tmux mode.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("""
[shell]
use_tmux = true
""")

    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background", "--debug"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    combined = result.stdout + result.stderr
    assert result.returncode == 0, f"Shell should start successfully. Output:\n{combined}"
    assert "Direct" not in combined, (
        f"Expected tmux mode (not direct) with use_tmux=true. Output:\n{combined}"
    )
