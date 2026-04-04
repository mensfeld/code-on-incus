"""
Test the built-in default profile.

Tests that:
1. Default profile appears in profile list with (built-in) source
2. Default profile show displays all default settings
3. Custom profiles can inherit from default
4. Disk-based default profile overrides built-in
"""

import subprocess
from pathlib import Path


def test_default_profile_in_list(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi profile list shows the built-in default profile.
    """
    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "list",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile list should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "default" in output, f"Should show 'default' profile. Got:\n{output}"
    assert "(built-in)" in output, f"Should show '(built-in)' source. Got:\n{output}"
    assert "coi-default" in output, f"Should show 'coi-default' image. Got:\n{output}"


def test_default_profile_show(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that coi profile show default displays all default settings.
    """
    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "info",
            "default",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile show default should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr

    # Should show key settings
    assert "coi-default" in output, f"Should show image. Got:\n{output}"
    assert "(built-in)" in output, f"Should show source. Got:\n{output}"
    assert "[tool]" in output, f"Should show tool section. Got:\n{output}"
    assert "claude" in output, f"Should show tool name. Got:\n{output}"
    assert "[network]" in output, f"Should show network section. Got:\n{output}"
    assert "mode =" in output, f"Should show network mode. Got:\n{output}"
    assert "[git]" in output, f"Should show git section. Got:\n{output}"
    assert "[ssh]" in output, f"Should show ssh section. Got:\n{output}"
    assert "[security]" in output, f"Should show security section. Got:\n{output}"
    assert "[timezone]" in output, f"Should show timezone section. Got:\n{output}"
    assert "host" in output, f"Should show timezone mode. Got:\n{output}"
    assert "[paths]" in output, f"Should show paths section. Got:\n{output}"
    assert "[incus]" in output, f"Should show incus section. Got:\n{output}"


def test_inherit_from_default(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a custom profile can inherit from the built-in default profile.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "custom"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('inherits = "default"\nimage = "my-custom-image"\n')

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "info",
            "custom",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile show custom should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "my-custom-image" in output, f"Should show custom image. Got:\n{output}"
    assert "default" in output, f"Should show inherits from default. Got:\n{output}"
    # Should inherit tool from default
    assert "[tool]" in output, f"Should inherit tool section from default. Got:\n{output}"


def test_disk_default_overrides_builtin(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a disk-based 'default' profile overrides the built-in one.
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "default"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('image = "my-disk-default"\n')

    result = subprocess.run(
        [
            coi_binary,
            "profile",
            "info",
            "default",
            "--workspace",
            workspace_dir,
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile show default should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "my-disk-default" in output, f"Should show disk-based image. Got:\n{output}"
