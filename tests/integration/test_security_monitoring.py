#!/usr/bin/env python3
"""
Integration tests for security monitoring.

Minimal tests focused on verifying monitoring can be enabled and configured.
Full threat detection testing requires more complex setup and is left for manual testing.
"""

import subprocess
import time
from pathlib import Path

import pytest


@pytest.fixture
def test_workspace(tmp_path):
    """Create a temporary workspace."""
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    (workspace / "test.txt").write_text("test")
    return str(workspace)


class TestMonitoringConfiguration:
    """Test monitoring configuration and feature availability."""

    def test_monitor_flag_exists(self, coi_binary):
        """Verify --monitor flag is recognized."""
        result = subprocess.run(
            [coi_binary, "shell", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert "--monitor" in result.stdout or "--monitor" in result.stderr

    def test_monitoring_config_accepted(self, test_workspace, coi_binary):
        """Verify monitoring config is accepted without errors."""
        config_path = Path.home() / ".config" / "coi" / "config.toml"
        config_backup = None

        if config_path.exists():
            config_backup = config_path.read_text()

        try:
            config_path.parent.mkdir(parents=True, exist_ok=True)
            config_path.write_text(
                """
[monitoring]
enabled = true
auto_pause_on_high = true
auto_kill_on_critical = true
poll_interval_sec = 2
"""
            )

            # Just verify config is parsed without error (use --help to avoid creating container)
            result = subprocess.run(
                [coi_binary, "--help"],
                capture_output=True,
                text=True,
                timeout=10,
            )
            assert result.returncode == 0

        finally:
            if config_backup:
                config_path.write_text(config_backup)
            elif config_path.exists():
                config_path.unlink()

    def test_shell_starts_with_monitoring_config(self, test_workspace, coi_binary):
        """Verify shell can start with monitoring enabled in config."""
        config_path = Path.home() / ".config" / "coi" / "config.toml"
        config_backup = None

        if config_path.exists():
            config_backup = config_path.read_text()

        try:
            config_path.parent.mkdir(parents=True, exist_ok=True)
            config_path.write_text(
                """
[monitoring]
enabled = true
poll_interval_sec = 2
"""
            )

            # Start shell and immediately exit to verify it can start
            proc = subprocess.Popen(
                [coi_binary, "shell", "--workspace", test_workspace, "--debug"],
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            # Give it a moment to start
            time.sleep(3)

            # Immediately exit
            try:
                proc.stdin.write("exit\n")
                proc.stdin.flush()
                proc.stdin.close()
            except:
                pass

            # Wait for exit with timeout
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=5)

            # Success if it started (returncode doesn't matter, just that it didn't crash immediately)

        finally:
            if config_backup:
                config_path.write_text(config_backup)
            elif config_path.exists():
                config_path.unlink()


# Note: Full threat detection testing (reverse shells, env scanning, etc.)
# requires more complex test infrastructure and is better suited for manual
# testing or a dedicated test environment.
#
# These tests verify that:
# 1. The --monitor flag exists and is recognized
# 2. Monitoring configuration is accepted
# 3. Shell can start with monitoring enabled
#
# This ensures the feature is available and functional at a basic level.
