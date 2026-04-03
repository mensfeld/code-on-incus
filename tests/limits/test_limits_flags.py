"""
Test limits configuration precedence.

Tests that:
1. Config file values are applied correctly
2. Profile values override global config values
3. Environment variables work alongside config
4. Precedence chain: Profile > Config > Env > Default
"""

import os
import subprocess
from pathlib import Path


def test_config_values_applied(coi_binary, workspace_dir, cleanup_containers):
    """Test that config file limit values are applied correctly."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with limits
    project_config_dir = Path(workspace_dir) / ".coi"
    project_config_dir.mkdir(exist_ok=True)
    (project_config_dir / "config.toml").write_text(
        """
[limits.cpu]
count = "2"

[limits.memory]
limit = "2GiB"
"""
    )

    # Launch without any extra flags -- config should be applied
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify config values were applied
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "2"' in config_output, "Config CPU limit should be applied"
    assert "limits.memory: 2GiB" in config_output, "Config memory limit should be applied"


def test_profile_overrides_config(coi_binary, workspace_dir, cleanup_containers):
    """Test that profile settings override global config."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with global limits
    project_config_dir = Path(workspace_dir) / ".coi"
    project_config_dir.mkdir(exist_ok=True)
    project_config = project_config_dir / "config.toml"
    config_content = """
[limits.cpu]
count = "4"

[limits.memory]
limit = "4GiB"
"""
    project_config.write_text(config_content)

    # Create directory profile with overriding limits
    profile_dir = project_config_dir / "profiles" / "limited"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
image = "coi-default"

[limits.cpu]
count = "1"

[limits.memory]
limit = "512MiB"
"""
    )

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


def test_env_vars_alongside_config(coi_binary, workspace_dir, cleanup_containers):
    """Test that environment variables work alongside config."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    env = os.environ.copy()
    env["COI_LIMIT_CPU"] = "1"
    env["COI_LIMIT_MEMORY"] = "512MiB"

    # Launch with env vars, no config file
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
        env=env,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify env var limits were applied
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "1"' in config_output, "Env var CPU limit should be applied"
    assert "limits.memory: 512MiB" in config_output, "Env var memory limit should be applied"


def test_config_overrides_env_vars(coi_binary, workspace_dir, cleanup_containers):
    """Test that config file settings override environment variables."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with limits
    project_config_dir = Path(workspace_dir) / ".coi"
    project_config_dir.mkdir(exist_ok=True)
    (project_config_dir / "config.toml").write_text(
        """
[limits.cpu]
count = "4"

[limits.memory]
limit = "4GiB"
"""
    )

    env = os.environ.copy()
    env["COI_LIMIT_CPU"] = "1"
    env["COI_LIMIT_MEMORY"] = "512MiB"

    # Launch with both config and env vars
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
        env=env,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify config took precedence over env vars
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "4"' in config_output, (
        "Config CPU limit should override env var (should be 4, not 1)"
    )
    assert "limits.memory: 4GiB" in config_output, (
        "Config memory limit should override env var (should be 4GiB, not 512MiB)"
    )


def test_config_with_multiple_limit_sections(coi_binary, workspace_dir, cleanup_containers):
    """Test that config with multiple limit sections is applied correctly."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with multiple limit sections
    project_config_dir = Path(workspace_dir) / ".coi"
    project_config_dir.mkdir(exist_ok=True)
    (project_config_dir / "config.toml").write_text(
        """
[limits.cpu]
count = "4"

[limits.memory]
limit = "4GiB"

[limits.runtime]
max_processes = 100
"""
    )

    # Launch without any extra flags
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify all config values
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "4"' in config_output, "CPU should be set from config"
    assert "limits.memory: 4GiB" in config_output, "Memory should be set from config"
    assert 'limits.processes: "100"' in config_output, "Processes should be set from config"


def test_profile_partial_override_of_config(coi_binary, workspace_dir, cleanup_containers):
    """Test that profile can partially override config (only specified values override)."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create project config with multiple limits
    project_config_dir = Path(workspace_dir) / ".coi"
    project_config_dir.mkdir(exist_ok=True)
    (project_config_dir / "config.toml").write_text(
        """
[limits.cpu]
count = "4"

[limits.memory]
limit = "4GiB"

[limits.runtime]
max_processes = 100
"""
    )

    # Create directory profile that only overrides CPU
    profile_dir = project_config_dir / "profiles" / "partial"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
image = "coi-default"

[limits.cpu]
count = "2"
"""
    )

    # Launch with profile (only CPU should be overridden)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "partial",
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
    assert 'limits.cpu: "2"' in config_output, "CPU should be overridden to 2 by profile"
    assert "limits.memory: 4GiB" in config_output, "Memory should remain from config (4GiB)"
    assert 'limits.processes: "100"' in config_output, "Processes should remain from config (100)"
