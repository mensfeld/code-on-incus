"""
Integration test: background IP refresh output must not appear in the terminal.

Regression test for https://github.com/mensfeld/code-on-incus/issues/372.

In allowlist mode the Manager starts a background goroutine that periodically
re-resolves allowed domains and updates firewall rules.  Before the fix that
goroutine used the global log package (stderr), which is attached to the
running tmux/shell session, causing every refresh cycle to dump log lines
directly into the user's terminal.

After the SessionLogger migration, all network manager logging goes to
~/.coi/logs/<container>.stdout.log and ~/.coi/logs/<container>.stderr.log
rather than the terminal.

What we verify:
  1. Startup stderr/stdout does NOT contain background-goroutine-only messages
     ("checking for updated IPs", "no changes detected", "next check in").
  2. The one-time setup message "Starting IP refresh" IS present in the
     session log file (~/.coi/logs/<container>.stdout.log).
  3. The session log file ~/.coi/logs/<container>.stdout.log is created so
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


def test_network_setup_logs_captured_in_session_log(coi_binary, workspace_dir, cleanup_containers):
    """
    One-time setup messages (e.g. "Starting IP refresh") are written to the
    session log file (~/.coi/logs/<container>.stdout.log) rather than the
    terminal after the SessionLogger migration.
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

        # Extract container name from terminal output.
        container_name = None
        for line in (result.stdout + result.stderr).split("\n"):
            if "Container:" in line or "Container name:" in line:
                parts = line.split(":", 1)
                if len(parts) == 2:
                    container_name = parts[1].strip()
                    break

        assert container_name, (
            f"Could not find container name in output:\n{result.stdout + result.stderr}"
        )

        # Poll until the session stdout log file exists (created asynchronously).
        log_path = os.path.expanduser(f"~/.coi/logs/{container_name}.stdout.log")
        deadline = time.time() + 15
        while not os.path.exists(log_path) and time.time() < deadline:
            time.sleep(0.5)

        assert os.path.exists(log_path), f"Expected session log file to exist at {log_path}"

        with open(log_path) as fh:
            log_contents = fh.read()

        assert "Starting IP refresh" in log_contents, (
            f"Expected 'Starting IP refresh' in session log {log_path}.\n"
            f"Log contents:\n{log_contents}"
        )

    finally:
        os.unlink(config_file)


def test_refresh_log_file_created(coi_binary, workspace_dir, cleanup_containers):
    """
    Session logger output must be written to
    ~/.coi/logs/<container>.stdout.log, not discarded.
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
            if "Container:" in line or "Container name:" in line:
                parts = line.split(":", 1)
                if len(parts) == 2:
                    container_name = parts[1].strip()
                    break

        assert container_name, (
            f"Could not find container name in output:\n{result.stdout + result.stderr}"
        )

        # The session log file is created at container startup.
        log_file = os.path.expanduser(f"~/.coi/logs/{container_name}.stdout.log")

        # Poll briefly — the file is created asynchronously but almost instantly.
        deadline = time.time() + 10
        while not os.path.exists(log_file) and time.time() < deadline:
            time.sleep(0.5)

        assert os.path.exists(log_file), f"Expected session log file to be created at {log_file}"

    finally:
        os.unlink(config_file)
