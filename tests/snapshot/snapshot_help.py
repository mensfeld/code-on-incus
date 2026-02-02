"""
Test for coi snapshot command help output.

Tests that:
1. Main snapshot command shows help with all subcommands
2. Each subcommand shows appropriate help
"""

import subprocess


def test_snapshot_help(coi_binary):
    """Test main snapshot command help."""
    result = subprocess.run(
        [coi_binary, "snapshot", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Help should succeed. stderr: {result.stderr}"

    # Check for key elements in help output
    assert "Manage Incus container snapshots" in result.stdout
    assert "create" in result.stdout
    assert "list" in result.stdout
    assert "restore" in result.stdout
    assert "delete" in result.stdout
    assert "info" in result.stdout


def test_snapshot_create_help(coi_binary):
    """Test snapshot create subcommand help."""
    result = subprocess.run(
        [coi_binary, "snapshot", "create", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Help should succeed. stderr: {result.stderr}"

    # Check for key flags
    assert "--container" in result.stdout or "-c" in result.stdout
    assert "--stateful" in result.stdout
    assert "auto-generated name" in result.stdout.lower() or "auto-named" in result.stdout.lower()


def test_snapshot_list_help(coi_binary):
    """Test snapshot list subcommand help."""
    result = subprocess.run(
        [coi_binary, "snapshot", "list", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Help should succeed. stderr: {result.stderr}"

    # Check for key flags
    assert "--container" in result.stdout or "-c" in result.stdout
    assert "--format" in result.stdout
    assert "--all" in result.stdout


def test_snapshot_restore_help(coi_binary):
    """Test snapshot restore subcommand help."""
    result = subprocess.run(
        [coi_binary, "snapshot", "restore", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Help should succeed. stderr: {result.stderr}"

    # Check for key flags and warnings
    assert "--container" in result.stdout or "-c" in result.stdout
    assert "--force" in result.stdout or "-f" in result.stdout
    assert "confirmation" in result.stdout.lower() or "stopped" in result.stdout.lower()


def test_snapshot_delete_help(coi_binary):
    """Test snapshot delete subcommand help."""
    result = subprocess.run(
        [coi_binary, "snapshot", "delete", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Help should succeed. stderr: {result.stderr}"

    # Check for key flags
    assert "--container" in result.stdout or "-c" in result.stdout
    assert "--force" in result.stdout or "-f" in result.stdout
    assert "--all" in result.stdout


def test_snapshot_info_help(coi_binary):
    """Test snapshot info subcommand help."""
    result = subprocess.run(
        [coi_binary, "snapshot", "info", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, f"Help should succeed. stderr: {result.stderr}"

    # Check for key flags
    assert "--container" in result.stdout or "-c" in result.stdout
    assert "--format" in result.stdout
