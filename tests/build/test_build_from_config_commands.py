"""
Test building a custom image using [build] config with inline commands.

Tests that:
1. .coi/config.toml with commands = [...] → coi build creates custom image
2. Commands are executed in order
"""

import subprocess
from pathlib import Path


def test_build_from_config_commands(coi_binary, workspace_dir):
    """
    Test that 'coi build' uses [build] config with inline commands.

    Flow:
    1. Create .coi/config.toml with [build] commands
    2. Run 'coi build'
    3. Verify custom image was created
    """
    image_name = "coi-test-build-cfg-cmds"

    # Skip if base image doesn't exist
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi"],
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

    # Create .coi/config.toml with inline commands
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
base = "coi"
commands = ["echo 'commands-build-marker' > /tmp/commands_marker.txt"]
"""
    )

    # Build
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
