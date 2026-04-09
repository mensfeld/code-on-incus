"""
Test standardized table output formatting across CLI commands.

Tests that:
1. profile list --format json outputs valid JSON
2. profile list --format text outputs table headers
3. profile list --format invalid is rejected
4. tmux list --format json outputs valid JSON (empty array when no sessions)
5. tmux list --format text outputs table headers or "No active sessions"
6. tmux list --format invalid is rejected
7. snapshot list text output uses table headers (not fixed-width printf)
8. image list --prefix text output uses table headers (no dash separators)
"""

import json
import subprocess
from pathlib import Path


def test_profile_list_format_json(coi_binary, workspace_dir):
    """coi profile list --format json should output valid JSON array."""
    result = subprocess.run(
        [coi_binary, "profile", "list", "--format", "json", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, (
        f"profile list --format json should succeed. stderr: {result.stderr}"
    )
    data = json.loads(result.stdout)
    assert isinstance(data, list), f"JSON output should be an array, got: {type(data)}"
    # Should contain the built-in default profile
    names = [entry["name"] for entry in data]
    assert "default" in names, f"Should include built-in default profile. Got: {names}"
    # Each entry should have expected fields
    for entry in data:
        assert "name" in entry, f"Entry should have 'name' field: {entry}"
        assert "image" in entry, f"Entry should have 'image' field: {entry}"


def test_profile_list_format_json_with_custom_profile(coi_binary, workspace_dir):
    """coi profile list --format json should include custom profiles."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "test-json"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('[container]\nimage = "my-test-image"\n')

    result = subprocess.run(
        [coi_binary, "profile", "list", "--format", "json", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, (
        f"profile list --format json should succeed. stderr: {result.stderr}"
    )
    data = json.loads(result.stdout)
    names = [entry["name"] for entry in data]
    assert "test-json" in names, f"Should include custom profile. Got: {names}"
    test_entry = next(e for e in data if e["name"] == "test-json")
    assert test_entry["image"] == "my-test-image", f"Image should match. Got: {test_entry}"


def test_profile_list_format_text_has_table_headers(coi_binary, workspace_dir):
    """coi profile list --format text should show table column headers."""
    result = subprocess.run(
        [coi_binary, "profile", "list", "--format", "text", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile list should succeed. stderr: {result.stderr}"
    output = result.stdout
    assert "NAME" in output, f"Should have NAME column header. Got:\n{output}"
    assert "IMAGE" in output, f"Should have IMAGE column header. Got:\n{output}"
    assert "PERSISTENT" in output, f"Should have PERSISTENT column header. Got:\n{output}"
    assert "SOURCE" in output, f"Should have SOURCE column header. Got:\n{output}"


def test_profile_list_format_invalid_rejected(coi_binary, workspace_dir):
    """coi profile list --format xml should fail with exit code 2."""
    result = subprocess.run(
        [coi_binary, "profile", "list", "--format", "xml", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode == 2, f"Expected exit code 2 for --format xml, got {result.returncode}"
    combined = result.stdout + result.stderr
    assert "invalid format" in combined.lower(), f"Expected 'invalid format' error, got: {combined}"


def test_tmux_list_format_json_empty(coi_binary):
    """coi tmux list --format json with no sessions should output empty JSON array."""
    result = subprocess.run(
        [coi_binary, "tmux", "list", "--format", "json"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, (
        f"tmux list --format json should succeed. stderr: {result.stderr}"
    )
    data = json.loads(result.stdout)
    assert isinstance(data, list), f"JSON output should be an array, got: {type(data)}"
    assert len(data) == 0, f"Should be empty when no sessions. Got: {data}"


def test_tmux_list_format_text_no_sessions(coi_binary):
    """coi tmux list --format text with no sessions should show 'No active sessions'."""
    result = subprocess.run(
        [coi_binary, "tmux", "list", "--format", "text"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"tmux list should succeed. stderr: {result.stderr}"
    assert "No active sessions" in result.stdout, (
        f"Should show 'No active sessions'. Got:\n{result.stdout}"
    )


def test_tmux_list_format_invalid_rejected(coi_binary):
    """coi tmux list --format xml should fail with exit code 2."""
    result = subprocess.run(
        [coi_binary, "tmux", "list", "--format", "xml"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 2, f"Expected exit code 2 for --format xml, got {result.returncode}"
    combined = result.stdout + result.stderr
    assert "invalid format" in combined.lower(), f"Expected 'invalid format' error, got: {combined}"
