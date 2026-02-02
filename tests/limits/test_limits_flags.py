"""
Test CLI flag precedence for limits.

Tests that:
1. CLI flags override config file settings
2. CLI flags override profile settings
3. CLI flags override environment variables
4. Precedence chain: CLI > Profile > Config > Env > Default
"""

import os
import subprocess
from pathlib import Path


def test_cli_flags_override_config(coi_binary, workspace_dir, cleanup_containers):
    """Test that CLI flags override config file settings."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with limits
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[limits.cpu]
count = "4"

[limits.memory]
limit = "4GiB"
"""
    project_config.write_text(config_content)

    # Launch with CLI flags that override config
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-cpu=2",
            "--limit-memory=2GiB",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify CLI flags took precedence
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "2"' in config_output, (
        "CLI flag CPU limit should override config (should be 2, not 4)"
    )
    assert "limits.memory: 2GiB" in config_output, (
        "CLI flag memory limit should override config (should be 2GiB, not 4GiB)"
    )


def test_cli_flags_override_profile(coi_binary, workspace_dir, cleanup_containers):
    """Test that CLI flags override profile settings."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with profile
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[profiles.limited]
image = "coi"

[profiles.limited.limits.cpu]
count = "1"

[profiles.limited.limits.memory]
limit = "512MiB"
"""
    project_config.write_text(config_content)

    # Launch with profile but override with CLI flags
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "limited",
            "--limit-cpu=4",
            "--limit-memory=4GiB",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify CLI flags took precedence over profile
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "4"' in config_output, (
        "CLI flag CPU limit should override profile (should be 4, not 1)"
    )
    assert "limits.memory: 4GiB" in config_output, (
        "CLI flag memory limit should override profile (should be 4GiB, not 512MiB)"
    )


def test_cli_flags_override_env_vars(coi_binary, workspace_dir, cleanup_containers):
    """Test that CLI flags override environment variables."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    env = os.environ.copy()
    env["COI_LIMIT_CPU"] = "1"
    env["COI_LIMIT_MEMORY"] = "512MiB"

    # Launch with CLI flags that override env vars
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-cpu=4",
            "--limit-memory=4GiB",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
        env=env,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify CLI flags took precedence over env vars
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "4"' in config_output, (
        "CLI flag CPU limit should override env var (should be 4, not 1)"
    )
    assert "limits.memory: 4GiB" in config_output, (
        "CLI flag memory limit should override env var (should be 4GiB, not 512MiB)"
    )


def test_profile_overrides_config(coi_binary, workspace_dir, cleanup_containers):
    """Test that profile settings override global config."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with both global and profile limits
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[limits.cpu]
count = "4"

[limits.memory]
limit = "4GiB"

[profiles.limited]
image = "coi"

[profiles.limited.limits.cpu]
count = "1"

[profiles.limited.limits.memory]
limit = "512MiB"
"""
    project_config.write_text(config_content)

    # Launch with profile (no CLI flags)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "limited",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify profile limits took precedence over global config
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "1"' in config_output, (
        "Profile CPU limit should override global config (should be 1, not 4)"
    )
    assert "limits.memory: 512MiB" in config_output, (
        "Profile memory limit should override global config (should be 512MiB, not 4GiB)"
    )


def test_partial_cli_override(coi_binary, workspace_dir, cleanup_containers):
    """Test that CLI flags can partially override config (only specified flags override)."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with multiple limits
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[limits.cpu]
count = "4"

[limits.memory]
limit = "4GiB"

[limits.runtime]
max_processes = 100
"""
    project_config.write_text(config_content)

    # Launch with only CPU override (memory and processes should come from config)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-cpu=2",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify partial override
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "2"' in config_output, "CPU should be overridden to 2"
    assert "limits.memory: 4GiB" in config_output, "Memory should remain from config (4GiB)"
    assert 'limits.processes: "100"' in config_output, "Processes should remain from config (100)"


def test_empty_cli_flag_does_not_override(coi_binary, workspace_dir, cleanup_containers):
    """Test that not specifying a CLI flag doesn't override config."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with limits
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[limits.cpu]
count = "2"

[limits.memory]
limit = "2GiB"
"""
    project_config.write_text(config_content)

    # Launch without any CLI limit flags
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify config limits are still applied
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "2"' in config_output, "Config CPU limit should be applied"
    assert "limits.memory: 2GiB" in config_output, "Config memory limit should be applied"
