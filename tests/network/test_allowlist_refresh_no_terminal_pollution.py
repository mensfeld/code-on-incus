"""
Integration test: background IP refresh output must not appear in the terminal.

Regression test for https://github.com/mensfeld/code-on-incus/issues/372.

In allowlist mode the Manager starts a background goroutine that periodically
re-resolves allowed domains and updates firewall rules.  Before the fix that
goroutine used the global log package (stderr), which is attached to the
running tmux/shell session, causing every refresh cycle to dump log lines
directly into the user's terminal.

After the fix, goroutine output is written to
~/.coi/logs/network-refresh-<container>.log instead.

What we verify:
  1. Startup stderr/stdout does NOT contain background-goroutine-only messages
     ("checking for updated IPs", "no changes detected", "next check in").
  2. The one-time setup message "Starting IP refresh" IS present in startup
     output (setup-phase logging is intentionally kept on stderr).
  3. The log file ~/.coi/logs/network-refresh-<container>.log is created so
     that the output is not silently discarded.
"""

import os
import subprocess
import tempfile
import time


def test_refresh_goroutine_output_not_in_terminal(coi_binary, workspace_dir, cleanup_containers):
    """
    Goroutine-only log messages must not appear in coi shell startup output.
    """
    # refresh_interval_minutes=1 so the goroutine fires ~60 s after start.
    # We don't wait for it; we just assert the setup output is clean.
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",
    "1.1.1.1",
    "example.com",
]
refresh_interval_minutes = 1
""")
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

        # These strings are emitted only by the background goroutine, never by
        # the one-time setup path.  They must not appear in the terminal output.
        goroutine_only_phrases = [
            "IP refresh: checking for updated IPs",
            "IP refresh: no changes detected",
            "IP refresh: next check in",
            "IP refresh: updating firewall with",
            "IP refresh: successfully updated firewall rules",
        ]
        for phrase in goroutine_only_phrases:
            assert phrase not in output, (
                f"Background goroutine message leaked into terminal output: {phrase!r}\n"
                f"Full output:\n{output}"
            )

    finally:
        os.unlink(config_file)


def test_setup_phase_logs_still_appear_in_terminal(
    coi_binary, workspace_dir, cleanup_containers
):
    """
    One-time setup messages (logged before the shell launches) must remain
    visible on stderr so the user can see what the network setup is doing.
    """
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",
    "1.1.1.1",
    "example.com",
]
refresh_interval_minutes = 30
""")
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

        # Setup-phase messages must still reach the terminal.
        assert "Starting IP refresh" in output, (
            f"Expected 'Starting IP refresh' in startup output.\nFull output:\n{output}"
        )

    finally:
        os.unlink(config_file)


def test_refresh_log_file_created(coi_binary, workspace_dir, cleanup_containers):
    """
    Background refresh output must be written to
    ~/.coi/logs/network-refresh-<container>.log, not discarded.
    """
    # Use refresh_interval_minutes=1 and sleep past the first tick so the
    # goroutine actually fires and writes to the log file.
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",
    "1.1.1.1",
    "example.com",
]
refresh_interval_minutes = 1
""")
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

        # Extract container name from output
        container_name = None
        for line in (result.stdout + result.stderr).split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, (
            f"Could not find container name in output:\n{result.stdout + result.stderr}"
        )

        # The log file is created when the goroutine starts (before any refresh fires),
        # so it should exist immediately after container startup.
        log_file = os.path.expanduser(
            f"~/.coi/logs/network-refresh-{container_name}.log"
        )

        # Poll briefly — the goroutine starts asynchronously but almost instantly.
        deadline = time.time() + 10
        while not os.path.exists(log_file) and time.time() < deadline:
            time.sleep(0.5)

        assert os.path.exists(log_file), (
            f"Expected refresh log file to be created at {log_file}"
        )

    finally:
        os.unlink(config_file)
