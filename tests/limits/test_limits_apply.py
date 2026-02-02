"""
Test that limits are actually applied to containers.

Tests that:
1. CPU limits are set correctly
2. Memory limits are set correctly
3. Disk I/O limits are set correctly
4. Process limits are set correctly
5. Multiple limits can be combined
"""

import subprocess
from pathlib import Path


def test_cpu_limit_applied(coi_binary, workspace_dir, cleanup_containers):
    """Test that CPU limits are applied to the container."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

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

    # Verify CPU limit in container config
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Should be able to read container config"
    assert 'limits.cpu: "2"' in result.stdout, (
        f"CPU limit should be set to 2. Config: {result.stdout}"
    )


def test_cpu_allowance_applied(coi_binary, workspace_dir, cleanup_containers):
    """Test that CPU allowance is applied to the container."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-cpu-allowance=50%",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify CPU allowance in container config
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Should be able to read container config"
    assert "limits.cpu.allowance: 50%" in result.stdout, (
        f"CPU allowance should be set to 50%. Config: {result.stdout}"
    )


def test_memory_limit_applied(coi_binary, workspace_dir, cleanup_containers):
    """Test that memory limits are applied to the container."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-memory=2GiB",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify memory limit in container config
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Should be able to read container config"
    assert "limits.memory: 2GiB" in result.stdout, (
        f"Memory limit should be set to 2GiB. Config: {result.stdout}"
    )


def test_memory_swap_applied(coi_binary, workspace_dir, cleanup_containers):
    """Test that memory swap configuration is applied."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-memory=1GiB",
            "--limit-memory-swap=false",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify memory swap in container config
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Should be able to read container config"
    assert 'limits.memory.swap: "false"' in result.stdout, (
        f"Memory swap should be disabled. Config: {result.stdout}"
    )


def test_disk_io_limits_applied(coi_binary, workspace_dir, cleanup_containers):
    """Test that disk I/O limits are applied to the container."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-disk-read=10MiB/s",
            "--limit-disk-write=5MiB/s",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify disk I/O limits in container config
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Should be able to read container config"
    assert "limits.read: 10MiB/s" in result.stdout, (
        f"Disk read limit should be set. Config: {result.stdout}"
    )
    assert "limits.write: 5MiB/s" in result.stdout, (
        f"Disk write limit should be set. Config: {result.stdout}"
    )


def test_process_limit_applied(coi_binary, workspace_dir, cleanup_containers):
    """Test that process limits are applied to the container."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-processes=100",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify process limit in container config
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Should be able to read container config"
    assert 'limits.processes: "100"' in result.stdout, (
        f"Process limit should be set to 100. Config: {result.stdout}"
    )


def test_multiple_limits_combined(coi_binary, workspace_dir, cleanup_containers):
    """Test that multiple limits can be applied together."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-cpu=2",
            "--limit-memory=2GiB",
            "--limit-disk-read=10MiB/s",
            "--limit-processes=100",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify all limits in container config
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Should be able to read container config"
    config = result.stdout

    assert 'limits.cpu: "2"' in config, "CPU limit should be set"
    assert "limits.memory: 2GiB" in config, "Memory limit should be set"
    assert "limits.read: 10MiB/s" in config, "Disk read limit should be set"
    assert 'limits.processes: "100"' in config, "Process limit should be set"


def test_cpu_priority_applied(coi_binary, workspace_dir, cleanup_containers):
    """Test that CPU priority is applied."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--limit-cpu=2",
            "--limit-cpu-priority=5",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify CPU priority in container config
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Should be able to read container config"
    assert 'limits.cpu.priority: "5"' in result.stdout, (
        f"CPU priority should be set to 5. Config: {result.stdout}"
    )


def test_limits_work_with_persistent_containers(coi_binary, workspace_dir, cleanup_containers):
    """Test that limits work with persistent containers."""
    container_name = f"coi-{Path(workspace_dir).name}-1"

    # Launch persistent container with limits
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--persistent",
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

    # Verify limits are applied
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, "Should be able to read container config"
    config = result.stdout

    assert 'limits.cpu: "2"' in config, "CPU limit should be set"
    assert "limits.memory: 2GiB" in config, "Memory limit should be set"

    # Run again with persistent container (should reuse)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--persistent",
            "echo",
            "test2",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    assert result.returncode == 0, f"Command should succeed. stderr: {result.stderr}"

    # Verify limits are still present
    result = subprocess.run(
        ["incus", "config", "show", container_name],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config = result.stdout
    assert 'limits.cpu: "2"' in config, "CPU limit should persist"
    assert "limits.memory: 2GiB" in config, "Memory limit should persist"
