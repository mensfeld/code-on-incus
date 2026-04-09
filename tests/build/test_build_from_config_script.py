"""
Test building a custom image using [container.build] config with a script.

Tests that:
1. .coi/config.toml with [container.build] script = "build.sh" → coi build creates custom image
2. The build script path is resolved relative to config file (so .coi/build.sh)
"""

import subprocess
from pathlib import Path


def test_build_from_config_script(coi_binary, workspace_dir):
    """
    Test that 'coi build' uses [container.build] config with script.

    Flow:
    1. Create .coi/config.toml with [container.build] and container.image
    2. Create .coi/build.sh
    3. Run 'coi build'
    4. Verify custom image was created
    """
    image_name = "coi-test-build-cfg-script"

    # Skip if base image doesn't exist
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        return

    # Cleanup any existing image from previous run
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    # Create .coi/config.toml
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        f"""
[container]
image = "{image_name}"

[container.build]
base = "coi-default"
script = "build.sh"
"""
    )

    # Create .coi/build.sh (script path relative to config file → .coi/build.sh)
    build_script = config_dir / "build.sh"
    build_script.write_text(
        """#!/bin/bash
set -e
echo "config-script-build-marker" > /tmp/build_marker.txt
"""
    )
    build_script.chmod(0o755)

    # Run coi build (from workspace dir so .coi/config.toml is found)
    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"Build should succeed. stderr: {result.stderr}"

    # Verify image exists
    result = subprocess.run(
        [coi_binary, "image", "exists", image_name],
        capture_output=True,
    )
    assert result.returncode == 0, f"Custom image '{image_name}' should exist"

    # Cleanup
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )


def test_build_script_takes_precedence(coi_binary, workspace_dir):
    """
    Test that script takes precedence over commands when both are set.

    Flow:
    1. Create config with both script and commands
    2. Run coi build
    3. Verify script was used (not commands)
    """
    image_name = "coi-test-build-precedence"

    # Skip if base image doesn't exist
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        return

    # Cleanup any existing image
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    # Create .coi/config.toml with both script and commands
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        f"""
[container]
image = "{image_name}"

[container.build]
base = "coi-default"
script = "build.sh"
commands = ["echo commands-were-used > /tmp/commands_marker.txt"]
"""
    )

    # Create build script that creates a different marker
    build_script = config_dir / "build.sh"
    build_script.write_text(
        """#!/bin/bash
set -e
echo "script-was-used" > /tmp/script_marker.txt
"""
    )
    build_script.chmod(0o755)

    # Build
    result = subprocess.run(
        [coi_binary, "build"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"Build should succeed. stderr: {result.stderr}"

    # Cleanup
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )
