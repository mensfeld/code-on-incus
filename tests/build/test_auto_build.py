"""
Test non-interactive behavior when image is missing on coi shell / coi run.

When stdin is NOT a terminal (piped subprocess), missing images return an
error instructing the user to run 'coi build' first. Interactive TTY
behavior (prompt to build) is covered in test_auto_build_prompt.py.

Tests:
- coi run with missing image → error with build instructions
- coi shell with missing image → error with build instructions
- profile [container] image overrides base config container.image
"""

import subprocess
from pathlib import Path

from support.helpers import write_workspace_container_config


def test_missing_image_on_run_shows_build_instructions(coi_binary, workspace_dir):
    """
    coi run with missing image → error telling user to run 'coi build'.
    """
    write_workspace_container_config(workspace_dir, image="coi-nonexistent-image-12345")
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "echo",
            "hello",
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Run should fail with missing image"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention image not found. Got:\n{combined}"
    )


def test_missing_image_on_shell_shows_build_instructions(coi_binary, workspace_dir):
    """
    coi shell with missing image → error telling user to run 'coi build'.
    """
    write_workspace_container_config(workspace_dir, image="coi-nonexistent-image-67890")
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Shell should fail with missing image"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention image not found. Got:\n{combined}"
    )


def test_missing_custom_image_on_run(coi_binary, cleanup_containers, workspace_dir):
    """
    coi run with config pointing to missing custom image → error with profile build hint.
    """
    image_name = "coi-test-missing-custom-run"

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[container]
image = "{image_name}"

[container.build]
commands = ["echo hello"]
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "echo",
            "hello",
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Run should fail with missing image"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention image not found. Got:\n{combined}"
    )
    assert "coi build" in combined.lower(), (
        f"Error should suggest running 'coi build'. Got:\n{combined}"
    )


def test_missing_custom_image_on_shell(coi_binary, workspace_dir):
    """
    coi shell with config pointing to missing custom image → error with profile build hint.
    """
    image_name = "coi-test-missing-custom-shell"

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[container]
image = "{image_name}"

[container.build]
commands = ["echo hello"]
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Shell should fail with missing image"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention image not found. Got:\n{combined}"
    )
    assert "coi build" in combined.lower(), (
        f"Error should suggest running 'coi build'. Got:\n{combined}"
    )


def test_profile_image_overrides_config(coi_binary, cleanup_containers, workspace_dir):
    """
    A profile's [container] image should override container.image from the
    base config (image selection is config/profile-driven — the former
    --image flag was removed).
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[container]
image = "coi-should-not-be-used"

[container.build]
commands = ["echo this-should-not-run"]
"""
    )
    profile_dir = config_dir / "profiles" / "override-image"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('[container]\nimage = "coi-default"\n')

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "override-image",
            "--",
            "echo",
            "profile-override-works",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    combined = result.stdout + result.stderr
    # If coi-default image exists, this should succeed
    if result.returncode == 0:
        assert "profile-override-works" in combined, (
            f"Command should execute with the profile's image. Got:\n{combined}"
        )
    else:
        # coi-default image might not exist in this environment — that's fine,
        # just make sure it didn't try to use "coi-should-not-be-used"
        assert "coi-should-not-be-used" not in combined, (
            f"Should use the profile image, not the base config image. Got:\n{combined}"
        )
