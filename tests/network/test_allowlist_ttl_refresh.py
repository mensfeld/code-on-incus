"""
Integration tests for TTL-aware DNS refresh in allowlist mode.

Tests that allowlist mode logs TTL information during domain resolution
and that the dynamic refresh interval is computed from DNS TTLs.
"""

import os
import subprocess
import tempfile
import time


def test_allowlist_logs_ttl_information(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that allowlist mode logs TTL information during domain resolution.

    Verifies that the TTL-aware DNS resolution produces TTL-related log output.
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
            [
                coi_binary,
                "shell",
                "--workspace",
                workspace_dir,
                "--background",
            ],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

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

        # TTL information is now written to the session stdout log.
        log_path = os.path.expanduser(f"~/.coi/logs/{container_name}.stdout.log")
        deadline = time.time() + 15
        while not os.path.exists(log_path) and time.time() < deadline:
            time.sleep(0.5)

        assert os.path.exists(log_path), f"Expected session log file at {log_path}"

        with open(log_path) as fh:
            log_contents = fh.read()

        assert "TTL" in log_contents, (
            f"Expected TTL information in session log {log_path}.\nLog contents:\n{log_contents}"
        )

    finally:
        os.unlink(config_file)


def test_allowlist_logs_dynamic_refresh_interval(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that allowlist mode logs the dynamic refresh interval.

    Verifies that the refresher reports its chosen interval (which may be
    TTL-based rather than the fixed config value).
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
            [
                coi_binary,
                "shell",
                "--workspace",
                workspace_dir,
                "--background",
            ],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

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

        # "Starting IP refresh" and "TTL-based" are now written to the session stdout log.
        log_path = os.path.expanduser(f"~/.coi/logs/{container_name}.stdout.log")
        deadline = time.time() + 15
        while not os.path.exists(log_path) and time.time() < deadline:
            time.sleep(0.5)

        assert os.path.exists(log_path), f"Expected session log file at {log_path}"

        with open(log_path) as fh:
            log_contents = fh.read()

        assert "Starting IP refresh" in log_contents, (
            f"Expected refresh start message in session log {log_path}.\n"
            f"Log contents:\n{log_contents}"
        )
        assert "TTL-based" in log_contents, (
            f"Expected TTL-based indicator in session log {log_path}.\n"
            f"Log contents:\n{log_contents}"
        )

    finally:
        os.unlink(config_file)


def test_allowlist_domains_accessible_with_ttl_refresh(
    coi_binary, workspace_dir, cleanup_containers
):
    """
    Test that allowed domains remain accessible after setup with TTL-aware refresh.

    Regression test: ensures TTL-aware resolution does not break basic connectivity.
    """
    with tempfile.NamedTemporaryFile(mode="w", suffix=".toml", delete=False) as f:
        f.write("""
[network]
mode = "allowlist"
allowed_domains = [
    "8.8.8.8",
    "1.1.1.1",
    "registry.npmjs.org",
]
refresh_interval_minutes = 30
""")
        config_file = f.name

    try:
        env = os.environ.copy()
        env["COI_CONFIG"] = config_file

        result = subprocess.run(
            [
                coi_binary,
                "shell",
                "--workspace",
                workspace_dir,
                "--background",
            ],
            capture_output=True,
            text=True,
            timeout=90,
            env=env,
        )

        assert result.returncode == 0, f"Failed to start container: {result.stderr}"

        # Extract container name
        container_name = None
        output = result.stdout + result.stderr
        for line in output.split("\n"):
            if "Container: " in line:
                container_name = line.split("Container: ")[1].strip()
                break

        assert container_name, f"Could not find container name in output: {output}"

        # Fix DNS in container
        subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "bash",
                "-c",
                "echo 'nameserver 8.8.8.8' > /etc/resolv.conf",
            ],
            capture_output=True,
            timeout=10,
        )

        # Test: curl allowed domain (should work)
        result = subprocess.run(
            [
                coi_binary,
                "container",
                "exec",
                container_name,
                "--",
                "curl",
                "-I",
                "-m",
                "10",
                "https://registry.npmjs.org",
            ],
            capture_output=True,
            text=True,
            timeout=15,
        )

        assert result.returncode == 0, f"Failed to reach allowed domain: {result.stderr}"
        assert "HTTP" in result.stderr, f"No HTTP response from allowed domain: {result.stderr}"

    finally:
        os.unlink(config_file)
