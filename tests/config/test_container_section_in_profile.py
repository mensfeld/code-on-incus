"""
Test that [container] section in profile config loads correctly.

Tests that image, persistent, and storage_pool fields parse from a
profile-level [container] section.
"""

import subprocess
from pathlib import Path


def test_container_section_in_profile_image_persistent(
    coi_binary, cleanup_containers, workspace_dir
):
    """[container] image and persistent fields parse from profile config."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "myprof"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        '[container]\nimage = "coi-prof-image"\npersistent = true\n'
    )

    result = subprocess.run(
        [coi_binary, "profile", "info", "myprof", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-prof-image" in output, f"Should show profile image. Got:\n{output}"
    assert "true" in output.lower(), f"Should show persistent=true. Got:\n{output}"


def test_container_section_in_profile_storage_pool(coi_binary, cleanup_containers, workspace_dir):
    """[container] storage_pool field parses from profile config."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "poolprof"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        '[container]\nimage = "coi-default"\nstorage_pool = "fast-nvme"\n'
    )

    # profile list should succeed even though pool doesn't exist (validation
    # only fires at launch).
    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"profile list should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "poolprof" in output, f"Should list profile. Got:\n{output}"
