"""
Test profile inheritance CLI display.

Tests that profile show and profile list correctly display inheritance info.
"""

import subprocess
from pathlib import Path


def test_profile_inherits_shown_in_show(coi_binary, cleanup_containers, workspace_dir):
    """coi profile show displays the inherits field."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[container]\nimage = "coi-parent"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n[container]\nimage = "coi-child"\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "info", "child", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile show should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "child" in output
    assert "coi-child" in output
    assert "inherits" in output.lower() and "parent" in output, (
        f"profile show should display inherits = parent. Got:\n{output}"
    )


def test_profile_inherits_shown_in_list(coi_binary, cleanup_containers, workspace_dir):
    """coi profile list shows INHERITS column with parent name for child."""
    coi_dir = Path(workspace_dir) / ".coi" / "profiles"

    parent_dir = coi_dir / "parent"
    parent_dir.mkdir(parents=True)
    (parent_dir / "config.toml").write_text('[container]\nimage = "coi-parent"\n')

    child_dir = coi_dir / "child"
    child_dir.mkdir(parents=True)
    (child_dir / "config.toml").write_text(
        'inherits = "parent"\n[container]\nimage = "coi-child"\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile list should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "INHERITS" in output, f"Should show INHERITS column header. Got:\n{output}"
    # Verify the child row contains the parent name in the INHERITS column
    child_lines = [line for line in output.splitlines() if "child" in line]
    assert child_lines, f"Should include a row for child. Got:\n{output}"
    assert any("parent" in line for line in child_lines), (
        f"Child row should show parent in the INHERITS column. Got:\n{output}"
    )
