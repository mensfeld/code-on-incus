"""
Test profile-specific limits.

Tests that:
1. Profiles can define their own limits
2. Profile limits override global config limits
3. Multiple profiles can have different limits
4. Profile limits work with CLI flag overrides
"""

import subprocess
from pathlib import Path


def test_profile_with_limits(coi_binary, workspace_dir, cleanup_containers):
    """Test that profile limits are applied when using a profile."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create config with profile that has limits
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[profiles.limited]
image = "coi"
persistent = false

[profiles.limited.limits.cpu]
count = "2"
allowance = "50%"

[profiles.limited.limits.memory]
limit = "2GiB"
enforce = "hard"

[profiles.limited.limits.disk]
read = "10MiB/s"

[profiles.limited.limits.runtime]
max_processes = 50
"""
    project_config.write_text(config_content)

    # Launch with profile
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

    # Verify all profile limits were applied
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "2"' in config_output, "Profile CPU count should be applied"
    assert "limits.cpu.allowance: 50%" in config_output, "Profile CPU allowance should be applied"
    assert "limits.memory: 2GiB" in config_output, "Profile memory limit should be applied"
    assert "limits.memory.enforce: hard" in config_output, (
        "Profile memory enforce should be applied"
    )
    assert "limits.read: 10MiB/s" in config_output, "Profile disk read limit should be applied"
    assert 'limits.processes: "50"' in config_output, "Profile process limit should be applied"


def test_multiple_profiles_different_limits(coi_binary, workspace_dir, cleanup_containers):
    """Test that different profiles can have different limits."""
    # Create config with two profiles with different limits
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[profiles.small]
image = "coi"

[profiles.small.limits.cpu]
count = "1"

[profiles.small.limits.memory]
limit = "512MiB"

[profiles.large]
image = "coi"

[profiles.large.limits.cpu]
count = "4"

[profiles.large.limits.memory]
limit = "8GiB"
"""
    project_config.write_text(config_content)

    # Test small profile
    container_name_1 = f"coi-{Path(workspace_dir).name}-1"
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot=1",
            "--profile",
            "small",
            "echo",
            "small",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Small profile should succeed. stderr: {result.stderr}"

    result = subprocess.run(
        ["incus", "config", "show", container_name_1],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "1"' in config_output, "Small profile should have 1 CPU"
    assert "limits.memory: 512MiB" in config_output, "Small profile should have 512MiB memory"

    # Clean up first container
    subprocess.run(
        [coi_binary, "container", "delete", container_name_1, "--force"],
        capture_output=True,
        timeout=30,
    )

    # Test large profile
    container_name_2 = f"coi-{Path(workspace_dir).name}-2"
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--slot=2",
            "--profile",
            "large",
            "echo",
            "large",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Large profile should succeed. stderr: {result.stderr}"

    result = subprocess.run(
        ["incus", "config", "show", container_name_2],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "4"' in config_output, "Large profile should have 4 CPUs"
    assert "limits.memory: 8GiB" in config_output, "Large profile should have 8GiB memory"


def test_profile_partial_limits(coi_binary, workspace_dir, cleanup_containers):
    """Test that profile can define only some limits (others remain unset)."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create profile with only CPU limit
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[profiles.cpu_only]
image = "coi"

[profiles.cpu_only.limits.cpu]
count = "2"
"""
    project_config.write_text(config_content)

    # Launch with profile
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "cpu_only",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify only CPU limit is set
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "2"' in config_output, "CPU limit should be set from profile"
    # Memory should not be set (or be empty)
    assert "limits.memory:" not in config_output or 'limits.memory: ""' in config_output, (
        "Memory limit should not be set (profile didn't specify it)"
    )


def test_profile_limits_with_global_config(coi_binary, workspace_dir, cleanup_containers):
    """Test interaction between global config limits and profile limits."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create config with both global and profile limits
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
# Global limits
[limits.cpu]
count = "4"

[limits.memory]
limit = "8GiB"

[limits.runtime]
max_processes = 200

# Profile with partial overrides
[profiles.override]
image = "coi"

[profiles.override.limits.cpu]
count = "1"

# Profile doesn't override memory or processes, so global should apply
"""
    project_config.write_text(config_content)

    # Launch with profile
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "override",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify profile CPU override but global memory/processes
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "1"' in config_output, "Profile should override CPU (should be 1, not 4)"
    assert "limits.memory: 8GiB" in config_output, (
        "Global memory should apply (profile didn't override)"
    )
    assert 'limits.processes: "200"' in config_output, (
        "Global process limit should apply (profile didn't override)"
    )


def test_cli_flags_override_profile_limits(coi_binary, workspace_dir, cleanup_containers):
    """Test that CLI flags can override profile limits."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create profile with limits
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[profiles.base]
image = "coi"

[profiles.base.limits.cpu]
count = "2"

[profiles.base.limits.memory]
limit = "2GiB"
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
            "base",
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

    # Verify CLI flags overrode profile
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "4"' in config_output, (
        "CLI flag should override profile CPU (should be 4, not 2)"
    )
    assert "limits.memory: 4GiB" in config_output, (
        "CLI flag should override profile memory (should be 4GiB, not 2GiB)"
    )


def test_profile_without_limits_uses_global(coi_binary, workspace_dir, cleanup_containers):
    """Test that profile without limits falls back to global config."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Create config with global limits and profile without limits
    project_config = Path(workspace_dir) / ".coi.toml"
    config_content = """
[limits.cpu]
count = "2"

[limits.memory]
limit = "2GiB"

[profiles.nolimits]
image = "coi"
persistent = false
# No limits defined in profile
"""
    project_config.write_text(config_content)

    # Launch with profile that has no limits
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "nolimits",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify global limits were applied
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_output = result.stdout
    assert 'limits.cpu: "2"' in config_output, "Global CPU limit should apply"
    assert "limits.memory: 2GiB" in config_output, "Global memory limit should apply"
