"""
Test that effort_level config controls whether CLAUDE_CODE_EFFORT_LEVEL is injected.

Covers issue #376: coi was always injecting effort_level = "medium" (and setting
CLAUDE_CODE_EFFORT_LEVEL) even when effort_level was not set in config. This
prevented users from changing the effort level interactively inside Claude because
Claude treats CLAUDE_CODE_EFFORT_LEVEL as authoritative and refuses overrides.

Tests that:
1. When effort_level is NOT set in config, settings.json does NOT contain
   effortLevel or CLAUDE_CODE_EFFORT_LEVEL — user can change it interactively.
2. When effort_level IS set in config (e.g. "high"), settings.json DOES contain
   the correct effortLevel and CLAUDE_CODE_EFFORT_LEVEL values.
"""

import subprocess
import time
from pathlib import Path

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_text_in_monitor,
    with_live_screen,
)

SETTINGS_PATH = "/home/code/.claude/settings.json"


def _read_settings(child):
    """Read settings.json from inside the container and return its text content."""
    marker = "SETTINGS_DUMP_END"
    with with_live_screen(child) as monitor:
        time.sleep(0.5)
        child.send(f"cat {SETTINGS_PATH} && echo {marker}\r")
        time.sleep(1)
        wait_for_text_in_monitor(monitor, marker, timeout=15)
        return monitor.get_text()


def _kill_session(child, coi_binary, container_name):
    child.send("sudo poweroff\r")
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


def test_effort_level_not_injected_when_unset(coi_binary, cleanup_containers, workspace_dir):
    """
    When effort_level is not set in config, settings.json must NOT contain
    effortLevel or CLAUDE_CODE_EFFORT_LEVEL so the user can change it interactively.
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text('[tool]\nname = "claude"\n')

    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir, env=env, timeout=120)
    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    # Exit Claude/dummy to bash
    child.send("exit\r")
    time.sleep(2)

    # Use grep to produce a clear marker — grep exits 1 (no output) when absent
    with with_live_screen(child) as monitor:
        time.sleep(0.5)
        child.send(
            f"grep -q effortLevel {SETTINGS_PATH} "
            "&& echo HAS_EFFORT_LEVEL || echo NO_EFFORT_LEVEL\r"
        )
        time.sleep(1)
        no_effort = wait_for_text_in_monitor(monitor, "NO_EFFORT_LEVEL", timeout=15)

    _kill_session(child, coi_binary, container_name)

    assert no_effort, (
        "settings.json must NOT contain effortLevel when effort_level is not "
        "configured — user should be able to change it interactively inside Claude"
    )


def test_effort_level_injected_when_configured(coi_binary, cleanup_containers, workspace_dir):
    """
    When effort_level = "high" is set in config, settings.json MUST contain
    effortLevel = "high" and CLAUDE_CODE_EFFORT_LEVEL = "high".
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        '[tool]\nname = "claude"\n\n[tool.claude]\neffort_level = "high"\n'
    )

    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir, env=env, timeout=120)
    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    child.send("exit\r")
    time.sleep(2)

    with with_live_screen(child) as monitor:
        time.sleep(0.5)
        child.send(
            f"grep -q '\"effortLevel\"' {SETTINGS_PATH} "
            "&& echo HAS_EFFORT_LEVEL || echo NO_EFFORT_LEVEL\r"
        )
        time.sleep(1)
        has_effort = wait_for_text_in_monitor(monitor, "HAS_EFFORT_LEVEL", timeout=15)

    with with_live_screen(child) as monitor:
        time.sleep(0.5)
        child.send(f"grep -q '\"high\"' {SETTINGS_PATH} && echo HAS_HIGH || echo NO_HIGH\r")
        time.sleep(1)
        has_high = wait_for_text_in_monitor(monitor, "HAS_HIGH", timeout=15)

    _kill_session(child, coi_binary, container_name)

    assert has_effort, (
        "settings.json must contain effortLevel when effort_level = 'high' is configured"
    )
    assert has_high, (
        "settings.json must contain the value 'high' when effort_level = 'high' is configured"
    )
