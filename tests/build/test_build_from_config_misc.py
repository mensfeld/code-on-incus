"""
Miscellaneous build-from-config integration tests.

Tests:
- Build skip when image exists (no rebuild)
- Build --force when image exists (rebuild)
- Missing script → clear error
- No [build] section → builds base coi image (fallback)
"""

import subprocess
from pathlib import Path


def test_build_skip_when_image_exists(coi_binary, workspace_dir):
    """
    Test that 'coi build' skips when the image already exists.

    Flow:
    1. Build a custom image via config
    2. Run 'coi build' again
    3. Verify it skips (returns success, prints "already exists")
    """
    image_name = "coi-test-build-skip"

    # Skip if base image doesn't exist
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        return

    # Cleanup
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    # Create config
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
commands = ["echo built"]
"""
    )

    # First build
    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"First build should succeed. stderr: {result.stderr}"

    # Second build (should skip)
    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"Second build should succeed (skip). stderr: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "already exists" in combined.lower(), (
        f"Should mention image already exists. Got:\n{combined}"
    )

    # Cleanup
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )


def test_build_force_rebuild(coi_binary, workspace_dir):
    """
    Test that 'coi build --force' rebuilds even when image exists.
    """
    image_name = "coi-test-build-force"

    # Skip if base image doesn't exist
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        return

    # Cleanup
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    # Create config
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
commands = ["echo rebuilt"]
"""
    )

    # First build
    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )
    assert result.returncode == 0

    # Force rebuild
    result = subprocess.run(
        [coi_binary, "build", "--force"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"Force rebuild should succeed. stderr: {result.stderr}"
    combined = result.stdout + result.stderr
    assert "built successfully" in combined.lower(), f"Should confirm rebuild. Got:\n{combined}"

    # Cleanup
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )


def test_build_missing_script_error(coi_binary, workspace_dir):
    """
    Test that referencing a non-existent build script gives a clear error.
    """
    image_name = "coi-test-build-missing-script"

    # Create config pointing to non-existent script
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
script = "nonexistent-build.sh"
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Build should fail with missing script"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention script not found. Got:\n{combined}"
    )


def test_build_no_config_fallback(coi_binary, workspace_dir):
    """
    Test that 'coi build' without [build] section falls back to building base coi image.

    We just verify it doesn't crash and attempts to build the coi image.
    """
    # Create .coi/config.toml without [build] section
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
image = "coi-default"
"""
    )

    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )

    # Should succeed (or skip if coi already exists)
    combined = result.stdout + result.stderr
    # Either builds successfully or says image already exists
    assert result.returncode == 0, f"Build fallback should succeed. stderr: {result.stderr}"
    assert "coi" in combined.lower(), f"Should mention building coi image. Got:\n{combined}"
