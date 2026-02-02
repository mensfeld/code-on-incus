"""
Test limits validation.

Tests that:
1. Valid limit formats are accepted
2. Invalid limit formats are rejected with clear errors
3. Validation happens before container launch
"""

import subprocess


def test_valid_cpu_count_formats(coi_binary, workspace_dir, cleanup_containers):
    """Test that valid CPU count formats are accepted."""
    valid_formats = ["1", "2", "0-3", "0,1,3", "0-1,3"]

    for cpu_count in valid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-cpu={cpu_count}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, (
            f"CPU count '{cpu_count}' should be valid. stderr: {result.stderr}"
        )


def test_invalid_cpu_count_formats(coi_binary, workspace_dir):
    """Test that invalid CPU count formats are rejected."""
    invalid_formats = ["abc", "1.5", "-1", "1-", "a-b"]

    for cpu_count in invalid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-cpu={cpu_count}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode != 0, f"CPU count '{cpu_count}' should be invalid"
        assert "invalid" in result.stderr.lower() or "validation" in result.stderr.lower(), (
            f"Error message should indicate validation failure. stderr: {result.stderr}"
        )


def test_valid_memory_formats(coi_binary, workspace_dir, cleanup_containers):
    """Test that valid memory formats are accepted."""
    valid_formats = ["512MiB", "1GiB", "2GB", "50%"]

    for memory in valid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-memory={memory}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, f"Memory '{memory}' should be valid. stderr: {result.stderr}"


def test_invalid_memory_formats(coi_binary, workspace_dir):
    """Test that invalid memory formats are rejected."""
    invalid_formats = ["2", "abc", "2XB", "100%%"]

    for memory in invalid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-memory={memory}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode != 0, f"Memory '{memory}' should be invalid"
        assert "invalid" in result.stderr.lower() or "validation" in result.stderr.lower(), (
            f"Error message should indicate validation failure. stderr: {result.stderr}"
        )


def test_valid_cpu_allowance_formats(coi_binary, workspace_dir, cleanup_containers):
    """Test that valid CPU allowance formats are accepted."""
    valid_formats = ["50%", "25ms/100ms", "10ms/50ms"]

    for allowance in valid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-cpu-allowance={allowance}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, (
            f"CPU allowance '{allowance}' should be valid. stderr: {result.stderr}"
        )


def test_invalid_cpu_allowance_formats(coi_binary, workspace_dir):
    """Test that invalid CPU allowance formats are rejected."""
    invalid_formats = ["abc", "200", "50", "25/100"]

    for allowance in invalid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-cpu-allowance={allowance}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode != 0, f"CPU allowance '{allowance}' should be invalid"
        assert "invalid" in result.stderr.lower() or "validation" in result.stderr.lower(), (
            f"Error message should indicate validation failure. stderr: {result.stderr}"
        )


def test_valid_disk_io_formats(coi_binary, workspace_dir, cleanup_containers):
    """Test that valid disk I/O formats are accepted."""
    valid_formats = ["10MiB/s", "100KiB/s", "1GiB/s", "1000iops"]

    for io_rate in valid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-disk-read={io_rate}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, (
            f"Disk I/O '{io_rate}' should be valid. stderr: {result.stderr}"
        )


def test_invalid_disk_io_formats(coi_binary, workspace_dir):
    """Test that invalid disk I/O formats are rejected."""
    invalid_formats = ["fast", "10MB", "1000", "abc"]

    for io_rate in invalid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-disk-read={io_rate}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode != 0, f"Disk I/O '{io_rate}' should be invalid"
        assert "invalid" in result.stderr.lower() or "validation" in result.stderr.lower(), (
            f"Error message should indicate validation failure. stderr: {result.stderr}"
        )


def test_valid_duration_formats(coi_binary, workspace_dir, cleanup_containers):
    """Test that valid duration formats are accepted."""
    valid_formats = ["30s", "5m", "2h", "1h30m"]

    for duration in valid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-duration={duration}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, (
            f"Duration '{duration}' should be valid. stderr: {result.stderr}"
        )


def test_invalid_duration_formats(coi_binary, workspace_dir):
    """Test that invalid duration formats are rejected."""
    invalid_formats = ["2x", "-1h", "abc", "100"]

    for duration in invalid_formats:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-duration={duration}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode != 0, f"Duration '{duration}' should be invalid"
        assert "invalid" in result.stderr.lower() or "validation" in result.stderr.lower(), (
            f"Error message should indicate validation failure. stderr: {result.stderr}"
        )


def test_priority_range_validation(coi_binary, workspace_dir, cleanup_containers):
    """Test that priority values are validated (0-10)."""
    # Valid priorities
    for priority in [0, 5, 10]:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-cpu-priority={priority}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, (
            f"Priority {priority} should be valid. stderr: {result.stderr}"
        )

    # Invalid priorities
    for priority in [-1, 11, 100]:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                f"--limit-cpu-priority={priority}",
                "echo",
                "test",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode != 0, f"Priority {priority} should be invalid"
        assert "invalid" in result.stderr.lower() or "priority" in result.stderr.lower(), (
            f"Error message should indicate priority validation failure. stderr: {result.stderr}"
        )
