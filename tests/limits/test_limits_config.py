"""
Test limits configuration loading and merging.

Tests that:
1. Config file limits are loaded correctly
2. Profile limits override global limits
3. Environment variables work
4. Configuration hierarchy is respected
"""

import os
import subprocess
from pathlib import Path


def test_config_file_limits_loaded(coi_binary, workspace_dir, cleanup_containers):
    """Test that limits from config file are loaded and applied."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create config file with limits
    config_dir = Path.home() / ".config" / "coi"
    config_dir.mkdir(parents=True, exist_ok=True)
    config_file = config_dir / "config.toml"

    # Backup existing config if present
    backup_file = None
    if config_file.exists():
        backup_file = config_file.with_suffix(".toml.backup")
        config_file.rename(backup_file)

    try:
        config_content = """
[defaults]
image = "coi"

[incus]
project = "default"

[limits.cpu]
count = "2"

[limits.memory]
limit = "1GiB"

[limits.runtime]
max_processes = 100
"""
        config_file.write_text(config_content)

        # Launch container with coi run (quick test)
        result = subprocess.run(
            [coi_binary, "run", "--workspace", workspace_dir, "echo", "test"],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

        # Check that limits were applied by inspecting container config
        result = subprocess.run(
            ["incus", "config", "show", container_name],
            capture_output=True,
            text=True,
            timeout=30,
        )

        config_output = result.stdout
        assert 'limits.cpu: "2"' in config_output, "CPU limit should be applied"
        assert "limits.memory: 1GiB" in config_output, "Memory limit should be applied"
        assert 'limits.processes: "100"' in config_output, "Process limit should be applied"

    finally:
        # Restore original config
        if config_file.exists():
            config_file.unlink()
        if backup_file and backup_file.exists():
            backup_file.rename(config_file)


def test_profile_limits_override_global(coi_binary, workspace_dir, cleanup_containers):
    """Test that profile limits override global limits."""
    # Create project config with profile
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

    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Launch with profile
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--profile", "limited", "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Check that profile limits were applied (not global limits)
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "1"' in config_output, "Profile CPU limit should override global"
    assert "limits.memory: 512MiB" in config_output, "Profile memory limit should override global"


def test_environment_variables_work(coi_binary, workspace_dir, cleanup_containers):
    """Test that environment variables set limits."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    env = os.environ.copy()
    env["COI_LIMIT_CPU"] = "2"
    env["COI_LIMIT_MEMORY"] = "2GiB"

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
        env=env,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Check that env var limits were applied
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "2"' in config_output, "CPU limit from env should be applied"
    assert "limits.memory: 2GiB" in config_output, "Memory limit from env should be applied"


def test_empty_limits_means_unlimited(coi_binary, workspace_dir, cleanup_containers):
    """Test that empty/missing limits result in unlimited (no limits applied)."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Launch without any limits configured
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "echo", "test"],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Check that no custom limits were applied (Incus defaults)
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    # Should not have custom CPU/memory limits (only Docker security flags)
    # We're checking that we didn't explicitly set limits
    # Note: security.nesting and other Docker flags are OK, we're just checking resource limits
    assert 'limits.cpu: "' not in config_output or 'limits.cpu: ""' in config_output, (
        "No CPU limit should be set"
    )
