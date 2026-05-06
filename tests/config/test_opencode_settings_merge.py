"""
Test for opencode settings merging - verify sandbox settings are merged with user config.

Tests that:
1. User config in ~/.config/opencode/opencode.json is preserved when sandbox settings are merged
2. Sandbox settings are created when no host config exists (AlwaysSetupConfig = true)
"""

import json
import os
import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
)


def test_opencode_settings_merge_preserves_user_config(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """
    Test that opencode.json user config is preserved when sandbox settings are merged.

    Flow:
    1. Create ~/.config/opencode/opencode.json with user config (custom theme)
    2. Start coi shell with opencode tool
    3. Read container's opencode.json via incus exec
    4. Verify both user config AND sandbox settings (permission.*: allow) are present
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # Create .coi/config.toml to select opencode tool
    config_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(config_dir, exist_ok=True)
    config_path = os.path.join(config_dir, "config.toml")
    with open(config_path, "w") as f:
        f.write('[tool]\nname = "opencode"\n')

    # Create a fake home with opencode config
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    opencode_dir = fake_home / ".config" / "opencode"
    opencode_dir.mkdir(parents=True)

    user_config = {
        "theme": "dark",
        "customUserSetting": "should_be_preserved",
    }
    opencode_json = opencode_dir / "opencode.json"
    opencode_json.write_text(json.dumps(user_config, indent=2))

    env = {"COI_USE_DUMMY": "1", "HOME": str(fake_home)}

    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    time.sleep(5)

    # Read opencode.json from container
    result = subprocess.run(
        [
            "incus",
            "exec",
            container_name,
            "--",
            "cat",
            "/home/code/.config/opencode/opencode.json",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_content = result.stdout

    # Cleanup
    child.send("\x03")
    time.sleep(1)
    child.send("\x03")
    time.sleep(2)

    child.send("sudo poweroff")
    time.sleep(0.3)
    child.send("\x0d")

    try:
        child.expect(EOF, timeout=60)
    except TIMEOUT:
        pass

    try:
        child.close(force=False)
    except Exception:
        child.close(force=True)

    time.sleep(5)

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    # Assertions
    assert result.returncode == 0, (
        f"Failed to read opencode.json from container.\n"
        f"Exit code: {result.returncode}\n"
        f"Stderr: {result.stderr}"
    )

    try:
        container_config = json.loads(config_content)
    except json.JSONDecodeError as e:
        raise AssertionError(
            f"Failed to parse JSON from container opencode.json:\n{config_content}\n\nError: {e}"
        )

    # Verify user settings are preserved
    assert "theme" in container_config, (
        f"User setting 'theme' should be preserved. Got: {json.dumps(container_config, indent=2)}"
    )
    assert container_config["theme"] == "dark", (
        f"User setting 'theme' value should be preserved. Got: {container_config['theme']}"
    )
    assert "customUserSetting" in container_config, (
        f"User setting 'customUserSetting' should be preserved. Got: {json.dumps(container_config, indent=2)}"
    )

    # Verify sandbox settings are present
    assert "permission" in container_config, (
        f"Sandbox setting 'permission' should be present. Got: {json.dumps(container_config, indent=2)}"
    )
    assert container_config["permission"].get("*") == "allow", (
        f"Sandbox permission bypass should be set. Got: {container_config.get('permission', {})}"
    )

    print("✓ All assertions passed: User settings preserved AND sandbox settings added")


def test_opencode_settings_created_when_no_host_config(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """
    Test that opencode.json is created with sandbox settings even when no host config exists.

    Opencode has AlwaysSetupConfig() = true, so settings should be created regardless.

    Flow:
    1. Don't create any host opencode config
    2. Start coi shell with opencode tool
    3. Read container's opencode.json
    4. Verify sandbox settings are present
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # Create .coi/config.toml to select opencode tool
    config_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(config_dir, exist_ok=True)
    config_path = os.path.join(config_dir, "config.toml")
    with open(config_path, "w") as f:
        f.write('[tool]\nname = "opencode"\n')

    # Create a fake home WITHOUT opencode config
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()

    env = {"COI_USE_DUMMY": "1", "HOME": str(fake_home)}

    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    time.sleep(5)

    # Read opencode.json from container
    result = subprocess.run(
        [
            "incus",
            "exec",
            container_name,
            "--",
            "cat",
            "/home/code/.config/opencode/opencode.json",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    config_content = result.stdout

    # Cleanup
    child.send("\x03")
    time.sleep(1)
    child.send("\x03")
    time.sleep(2)

    child.send("sudo poweroff")
    time.sleep(0.3)
    child.send("\x0d")

    try:
        child.expect(EOF, timeout=60)
    except TIMEOUT:
        pass

    try:
        child.close(force=False)
    except Exception:
        child.close(force=True)

    time.sleep(5)

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )

    # Assertions
    assert result.returncode == 0, (
        f"Failed to read opencode.json from container.\n"
        f"Exit code: {result.returncode}\n"
        f"Stderr: {result.stderr}"
    )

    try:
        container_config = json.loads(config_content)
    except json.JSONDecodeError as e:
        raise AssertionError(
            f"Failed to parse JSON from container opencode.json:\n{config_content}\n\nError: {e}"
        )

    # Verify sandbox settings are present
    assert "permission" in container_config, (
        f"Sandbox setting 'permission' should be present. Got: {json.dumps(container_config, indent=2)}"
    )
    assert container_config["permission"].get("*") == "allow", (
        f"Sandbox permission bypass should be set. Got: {container_config.get('permission', {})}"
    )

    print("✓ All assertions passed: Sandbox settings created when no host config exists")
