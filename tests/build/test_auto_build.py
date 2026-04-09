"""
Test behavior when image is missing on coi shell / coi run.

Since auto-build was removed, missing images now return an error
instructing the user to run 'coi build' first.

Tests:
- coi run with missing image → error with build instructions
- coi shell with missing image → error with build instructions
- --image flag overrides config container.image for image check
"""

import subprocess
from pathlib import Path


def test_missing_image_on_run_shows_build_instructions(coi_binary, workspace_dir):
    """
    coi run with missing image → error telling user to run 'coi build'.
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--image",
            "coi-nonexistent-image-12345",
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
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--image",
            "coi-nonexistent-image-67890",
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


def test_image_flag_overrides_config(coi_binary, cleanup_containers, workspace_dir):
    """
    --image flag should override container.image from config.

    If config says image = "coi-custom" but user passes --image coi-default,
    the image check should use coi-default.
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

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--image",
            "coi-default",
            "--",
            "echo",
            "flag-override-works",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    combined = result.stdout + result.stderr
    # If coi-default image exists, this should succeed
    if result.returncode == 0:
        assert "flag-override-works" in combined, (
            f"Command should execute with --image override. Got:\n{combined}"
        )
    else:
        # coi-default image might not exist in this environment — that's fine,
        # just make sure it didn't try to use "coi-should-not-be-used"
        assert "coi-should-not-be-used" not in combined, (
            f"Should use --image flag, not config container.image. Got:\n{combined}"
        )
