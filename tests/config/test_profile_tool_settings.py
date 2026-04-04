"""
Test that profile tool settings are loaded and applied correctly.

Tests that:
1. Profile [tool] section is loaded and visible in profile show
2. Profile tool settings are applied to config when --profile is used
"""

import subprocess
from pathlib import Path


def test_profile_tool_settings_shown(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that profile tool settings appear in 'coi profile show'.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "tooltest"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[tool]
name = "aider"
permission_mode = "interactive"

[tool.claude]
effort_level = "high"
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "info",
            "tooltest",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile show should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "aider" in output, f"Should show tool name 'aider'. Got:\n{output}"
    assert "interactive" in output, f"Should show permission_mode 'interactive'. Got:\n{output}"
    assert "high" in output, f"Should show effort_level 'high'. Got:\n{output}"


def test_profile_tool_override_applied_at_runtime(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that using --profile with an invalid tool name fails at runtime,
    proving the profile's [tool] section is being applied to the config.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "badtool"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[tool]
name = "nonexistent-tool-xyz"
"""
    )

    # coi shell --profile should fail because the tool doesn't exist,
    # proving the profile's tool.name was applied
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--profile",
            "badtool",
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, f"Should fail with unknown tool. stdout: {result.stdout}"
    combined = result.stdout + result.stderr
    assert "nonexistent-tool-xyz" in combined, (
        f"Error should mention the bad tool name. Got:\n{combined}"
    )
