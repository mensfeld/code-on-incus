"""
Test auto-build on coi shell / coi run when image is missing.

Tests:
- coi run with missing image + [build] config → auto-builds, then runs
- Missing image + no [build] config → existing error message
"""

import subprocess
from pathlib import Path


def test_auto_build_on_run(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that 'coi run' auto-builds from config when image is missing.

    Flow:
    1. Create .coi/config.toml with [build] and defaults.image = custom name
    2. Ensure the custom image does NOT exist
    3. Run coi run -- echo hello
    4. Verify it auto-builds and then runs the command
    """
    image_name = "coi-test-auto-build-run"

    # Skip if base image doesn't exist
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi"],
        capture_output=True,
    )
    if result.returncode != 0:
        return

    # Ensure image does NOT exist
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    # Create config with build section
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
commands = ["echo auto-built"]
"""
    )

    # Run coi run (should auto-build then execute)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "echo",
            "auto-build-works",
        ],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )

    combined = result.stdout + result.stderr
    assert result.returncode == 0, (
        f"Run with auto-build should succeed. Output:\n{combined}"
    )

    # Verify auto-build happened
    assert "not found" in combined.lower() and "building" in combined.lower(), (
        f"Should mention auto-building. Got:\n{combined}"
    )

    # Verify command output
    assert "auto-build-works" in combined, (
        f"Command should have executed. Got:\n{combined}"
    )

    # Cleanup
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )


def test_no_auto_build_without_config(coi_binary, workspace_dir):
    """
    Test that missing image without [build] config gives standard error.

    Flow:
    1. No .coi/config.toml (or one without [build])
    2. Run coi run with --image=nonexistent
    3. Verify it fails with "not found" error
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
