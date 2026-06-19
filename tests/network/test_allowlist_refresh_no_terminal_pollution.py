"""
Integration tests: allowlist network diagnostics must never reach the terminal.

Regression tests for https://github.com/mensfeld/code-on-incus/issues/372.

In allowlist mode coi resolves the allowed domains at setup and a background
goroutine periodically re-resolves them (TTL-aware) and updates the nft rules.
Before the fix, the resolver and nft helpers logged via the global `log` package
(stderr), which in a running `coi shell` is the user's tmux terminal — so every
refresh dumped lines like `example.com: resolved 3 IPs (TTL: 300s)` straight into
the Claude session. (An earlier fix only redirected the refresh goroutine's own
"IP refresh: ..." lines, so the resolver flood persisted in 0.9.)

These tests lock the *behaviour*, not the code:
  1. setup-time network diagnostics do not appear in the terminal, and ARE
     written to the session log file (~/.coi/logs/<container>.stdout.log);
  2. a real background refresh fires in a live session and its output goes to the
     log file, NOT the attached terminal.
"""

import os
import subprocess
import tempfile
import time
from pathlib import Path

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
)

# Substrings emitted only by the network resolver / IP-refresh diagnostics.
# None of these must ever appear on the user's terminal. They are specific
# enough not to collide with the normal "[setup] ..." progress output.
LEAK_PATTERNS = [
    "IP refresh:",  # refresh goroutine progress
    "(TTL:",  # resolver: "<domain>: resolved N IPs (TTL: Xs)"
    ": resolved ",  # resolver per-domain resolution line
    "TTL-aware DNS",  # resolver fallback warning
    "Using cached IPs for",  # resolver cache notice
    "No cached IPs available",
    "updating nft rules with",
    "wildcard — resolving",
]

_ALLOWLIST_CONFIG = """
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",
    "1.1.1.1",
    "example.com",
]
refresh_interval_minutes = 1
"""


def _assert_no_leak(text, where):
    for pat in LEAK_PATTERNS:
        assert pat not in text, (
            f"network diagnostic {pat!r} leaked into {where} (issue #372).\nOutput:\n{text}"
        )


def _session_log_path(container_name):
    return os.path.expanduser(f"~/.coi/logs/{container_name}.stdout.log")


def _container_name_from_output(output):
    for line in output.split("\n"):
        if "Container:" in line or "Container name:" in line:
            parts = line.split(":", 1)
            if len(parts) == 2:
                return parts[1].strip()
    return None


def test_setup_diagnostics_not_in_terminal_but_in_log(
    coi_binary, workspace_dir, cleanup_containers
):
    """Setup-time resolver/nft diagnostics go to the session log, not the terminal."""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write(_ALLOWLIST_CONFIG)
        config_file = f.name

    try:
        env = os.environ.copy()
        env["COI_CONFIG"] = config_file

        result = subprocess.run(
            [coi_binary, "shell", "--workspace", workspace_dir, "--background"],
            capture_output=True,
            text=True,
            timeout=120,
            env=env,
        )
        assert result.returncode == 0, f"coi shell failed: {result.stderr}"

        output = result.stdout + result.stderr
        # The bug: resolver lines leaked here. Must be clean now.
        _assert_no_leak(output, "coi shell startup terminal output")

        # Positive side: the diagnostics must actually be routed to the log file
        # (so they're not merely suppressed/lost).
        container_name = _container_name_from_output(output)
        assert container_name, f"could not find container name in:\n{output}"
        log_path = _session_log_path(container_name)
        deadline = time.time() + 20
        while not os.path.exists(log_path) and time.time() < deadline:
            time.sleep(0.5)
        assert os.path.exists(log_path), f"expected session log at {log_path}"
        log = Path(log_path).read_text()
        assert "Starting IP refresh" in log, f"refresh setup not in session log:\n{log}"
        # At least one resolver line for the example.com domain must be present in
        # the log (proving the resolver output is routed there, not just hidden).
        assert (": resolved " in log) or ("(TTL:" in log) or ("Failed to resolve" in log), (
            f"resolver diagnostics not routed to session log:\n{log}"
        )
    finally:
        os.unlink(config_file)


def test_background_refresh_does_not_pollute_attached_terminal(
    coi_binary, workspace_dir, cleanup_containers
):
    """A real refresh fires in a live (attached) session; its output goes to the
    log file, never the terminal.

    This exercises the timing-dependent background refresh end to end: an
    interactive `coi shell` session is kept open long enough (refresh_interval=1)
    for the refresh goroutine to fire, then we assert the terminal received none
    of the diagnostics while the log file recorded the refresh.
    """
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write(_ALLOWLIST_CONFIG)
        config_file = f.name

    child = None
    try:
        env = {"COI_USE_DUMMY": "1", "COI_CONFIG": config_file}
        child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir, env=env, timeout=240)
        wait_for_container_ready(child, timeout=90)
        wait_for_prompt(child, timeout=120)

        container_name = calculate_container_name(workspace_dir, 1)
        log_path = _session_log_path(container_name)

        # Wait for at least one background refresh to actually fire (interval=1m,
        # TTL-capped). The refresh goroutine logs this marker to the session log.
        fired = False
        deadline = time.time() + 150
        while time.time() < deadline:
            if (
                os.path.exists(log_path)
                and "IP refresh: checking for updated IPs" in Path(log_path).read_text()
            ):
                fired = True
                break
            time.sleep(3)

        # Everything the attached terminal received so far (raw, pre-render, so
        # nothing scrolled off is missed).
        raw = ""
        lf = child.logfile_read
        for attr in ("get_raw_output", "get_display_stripped", "get_output"):
            if hasattr(lf, attr):
                raw = getattr(lf, attr)()
                break

        _assert_no_leak(raw, "attached coi shell terminal during a refresh")

        assert fired, (
            "background IP refresh did not fire within 150s, so the no-leak "
            f"assertion could not be validated against a real refresh.\nlog:\n"
            f"{Path(log_path).read_text() if os.path.exists(log_path) else '(no log)'}"
        )
        # The refresh's diagnostics must be in the log (routed, not lost).
        log = Path(log_path).read_text()
        assert "IP refresh:" in log, f"refresh output not routed to session log:\n{log}"
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
        os.unlink(config_file)
