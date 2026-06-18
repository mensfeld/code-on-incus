"""
Repro for issue #505: panic "slice bounds out of range [:12] with length 11"
on `coi shell --resume` when [monitoring] enabled = true.

Reported flow (0.9.0): a session created/resumed with monitoring enabled crashes
the coi binary at session start with a Go slice-bounds panic, right after
"Starting session..." / "[security] Process/filesystem monitoring started".
With monitoring disabled it works.

This test reproduces the real end-to-end scenario: it enables monitoring in
trusted-scope config (~/.coi/config.toml), creates an ephemeral dummy session,
powers it off (saving the session), then `coi shell --resume`. It asserts the
coi process does NOT panic (no "slice bounds out of range" / "panic:" in output)
and that the session actually resumes.

NOTE (placeholder): the exact trigger is being pinned down; assertions target the
panic signature from the issue so this test goes red on the bug and green on the
fix. Refine the trigger/assertions once the root cause is confirmed.
"""

import os
import subprocess
import time
from pathlib import Path

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    get_container_list,
    send_prompt,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
    wait_for_specific_container_deletion,
    wait_for_text_in_monitor,
    wait_for_text_on_screen,
    with_live_screen,
)

PANIC_MARKERS = ("slice bounds out of range", "panic:", "runtime error:")


def _write_monitoring_config():
    """Enable monitoring in trusted-scope config, mirroring the issue's settings.

    Returns (config_path, restore_fn). [network] mode=open avoids CI false-positive
    network threats; high file-read thresholds avoid startup-noise HIGH threats.
    """
    config_path = Path.home() / ".coi" / "config.toml"
    backup = config_path.read_text() if config_path.exists() else None
    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(
        """
[network]
mode = "open"

[monitoring]
enabled = true
auto_pause_on_high = true
auto_kill_on_critical = true
poll_interval_sec = 1
file_read_threshold_mb = 500
file_read_rate_mb_per_sec = 1000

[monitoring.nft]
enabled = false
"""
    )

    def restore():
        if backup is not None:
            config_path.write_text(backup)
        elif config_path.exists():
            config_path.unlink()

    return config_path, restore


def _raw_output(child):
    if hasattr(child.logfile_read, "get_raw_output"):
        return child.logfile_read.get_raw_output()
    if hasattr(child.logfile_read, "get_display_stripped"):
        return child.logfile_read.get_display_stripped()
    if hasattr(child.logfile_read, "get_output"):
        return child.logfile_read.get_output()
    return ""


def _assert_no_panic(output, where):
    lowered = output.lower()
    for marker in PANIC_MARKERS:
        assert marker not in lowered, (
            f"coi panicked during {where} (issue #505).\n"
            f"Found marker {marker!r} in output:\n{output}"
        )


def test_resume_with_monitoring_does_not_panic(coi_binary, cleanup_containers, workspace_dir):
    env = {"COI_USE_DUMMY": "1"}
    config_path, restore = _write_monitoring_config()

    try:
        # === Phase 1: create an ephemeral session with monitoring enabled ===
        child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir, env=env, timeout=120)
        wait_for_container_ready(child, timeout=90)
        wait_for_prompt(child, timeout=120)

        container_name = calculate_container_name(workspace_dir, 1)

        with with_live_screen(child) as monitor:
            time.sleep(2)
            send_prompt(child, "remember this message")
            assert wait_for_text_in_monitor(monitor, "remember this message-BACK", timeout=30), (
                "Dummy CLI should respond in the initial session"
            )

        # Exit CLI to bash, then poweroff to save the session.
        child.send("exit")
        time.sleep(0.3)
        child.send("\x0d")
        time.sleep(3)
        child.send("sudo poweroff")
        time.sleep(0.3)
        child.send("\x0d")
        try:
            child.expect(EOF, timeout=60)
        except TIMEOUT:
            pass

        output1 = _raw_output(child)
        try:
            child.close(force=False)
        except Exception:
            child.close(force=True)

        _assert_no_panic(output1, "initial session with monitoring")
        assert wait_for_specific_container_deletion(container_name, timeout=60), (
            f"Container {container_name} should be deleted after poweroff"
        )

        # === Phase 2: resume — this is where #505 panics ===
        child2 = spawn_coi(coi_binary, ["shell", "--resume"], cwd=workspace_dir, env=env, timeout=120)

        resumed = False
        try:
            wait_for_container_ready(child2, timeout=90)
            wait_for_text_on_screen(child2, "Resuming session", timeout=30)
            resumed = True
        except (TimeoutError, TIMEOUT, EOF):
            resumed = False

        output2 = _raw_output(child2)

        # Best-effort cleanup before assertions.
        try:
            child2.send("exit")
            time.sleep(0.3)
            child2.send("\x0d")
            time.sleep(2)
            child2.send("sudo poweroff")
            time.sleep(0.3)
            child2.send("\x0d")
            child2.expect(EOF, timeout=60)
        except (TIMEOUT, EOF, Exception):
            pass
        try:
            child2.close(force=False)
        except Exception:
            child2.close(force=True)

        container_name2 = calculate_container_name(workspace_dir, 1)
        if not wait_for_specific_container_deletion(container_name2, timeout=60):
            subprocess.run(
                [coi_binary, "container", "delete", container_name2, "--force"],
                capture_output=True,
                timeout=30,
            )
        time.sleep(1)
        assert container_name2 not in get_container_list(), (
            f"Container {container_name2} should be cleaned up"
        )

        # The actual repro assertions.
        _assert_no_panic(output2, "coi shell --resume with monitoring")
        assert resumed, f"Resume should succeed without crashing. Output:\n{output2}"
    finally:
        restore()
