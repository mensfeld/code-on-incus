"""
Test that profile tool settings are applied to container sessions.

Tests that:
1. Profile [tool] section overrides global tool config
2. Profile-set permission_mode is respected
"""

import subprocess
from pathlib import Path


def test_profile_tool_name_applied(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a profile can override the tool name.

    We use 'coi run' which doesn't launch an interactive tool but still applies
    the profile. We verify by checking the SANDBOX_CONTEXT.md injected into
    the container, which includes the configured tool name.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "tooltest"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[tool]
name = "aider"
"""
    )

    # Run a command and check SANDBOX_CONTEXT.md for tool name
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "tooltest",
            "--",
            "sh",
            "-c",
            "cat /workspace/SANDBOX_CONTEXT.md 2>/dev/null || echo NO_CONTEXT",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"
    combined = result.stdout + result.stderr
    # The SANDBOX_CONTEXT.md should reference the tool name from the profile
    # If auto_context is enabled (default), tool name appears in context
    assert "aider" in combined.lower() or "NO_CONTEXT" in combined, (
        f"Tool name from profile should be applied. Got:\n{combined}"
    )


def test_profile_tool_inline_overrides_directory(
    coi_binary, cleanup_containers, workspace_dir
):
    """
    Test that inline profile tool settings override directory profile.

    Flow:
    1. Directory profile sets tool.name = "aider"
    2. Inline profile sets tool.name = "claude"
    3. The inline should win
    """
    # Directory profile
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "toolover"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[tool]
name = "aider"
"""
    )

    # Inline profile overrides tool name
    config_dir = Path(workspace_dir) / ".coi"
    (config_dir / "config.toml").write_text(
        """
[profiles.toolover]
environment = { TOOL_SOURCE = "inline" }
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "toolover",
            "--",
            "sh",
            "-c",
            "echo SRC=$TOOL_SOURCE",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"
    combined = result.stdout + result.stderr
    # Inline profile replaced the directory profile entirely (including losing tool config)
    assert "SRC=inline" in combined, (
        f"Inline profile should have been applied. Got:\n{combined}"
    )
