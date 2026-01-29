"""
Test for settings.json merging - verify sandbox settings don't overwrite user config.

This test verifies the bug fix for GitHub issue #76 where settings.json
was being overwritten instead of merged, causing user configurations
(like AWS Bedrock settings) to be lost.

Tests that:
1. Create a host ~/.claude/settings.json with user config
2. Start coi shell (which should merge sandbox settings)
3. Verify container's settings.json contains BOTH user config AND sandbox settings
"""

import json
import subprocess
import time

from pexpect import EOF, TIMEOUT

from support.helpers import (
    calculate_container_name,
    spawn_coi,
    wait_for_container_ready,
    wait_for_prompt,
)


def test_settings_json_merge_preserves_user_config(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """
    Test that settings.json is merged, not overwritten.

    Flow:
    1. Create ~/.claude/settings.json with user config (simulating Bedrock setup)
    2. Start coi shell
    3. Exit claude to bash
    4. Read container's ~/.claude/settings.json
    5. Verify it contains BOTH user config AND sandbox settings
    6. Cleanup
    """
    env = {"COI_USE_DUMMY": "1"}
    container_name = calculate_container_name(workspace_dir, 1)

    # Create a fake home directory for this test with .claude/settings.json
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    claude_dir = fake_home / ".claude"
    claude_dir.mkdir()

    # Create settings.json with user config (simulating Bedrock setup)
    user_settings = {
        "awsAuthRefresh": "aws sso login --profile bedrock-users",
        "env": {
            "AWS_PROFILE": "bedrock-users",
            "AWS_REGION": "us-west-2",
            "CLAUDE_CODE_USE_BEDROCK": "true",
            "ANTHROPIC_DEFAULT_SONNET_MODEL": "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
        },
        "userCustomSetting": "should_be_preserved",
    }

    settings_file = claude_dir / "settings.json"
    settings_file.write_text(json.dumps(user_settings, indent=2))

    # Set HOME to point to our fake home directory
    env["HOME"] = str(fake_home)

    # === Phase 1: Start session ===

    child = spawn_coi(
        coi_binary,
        ["shell"],
        cwd=workspace_dir,
        env=env,
        timeout=120,
    )

    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    # Exit CLI to bash
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # === Phase 2: Check settings.json in container ===

    # Use incus exec to read the file directly from the container
    time.sleep(2)  # Give container a moment to be ready

    result = subprocess.run(
        [
            "sg",
            "incus-admin",
            "-c",
            f"incus exec {container_name} -- cat /home/code/.claude/settings.json",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    if result.returncode != 0:
        # Try to get some debug info
        debug_result = subprocess.run(
            [
                "sg",
                "incus-admin",
                "-c",
                f"incus exec {container_name} -- ls -la /home/code/.claude/",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )
        raise AssertionError(
            f"Failed to read settings.json from container.\n"
            f"Exit code: {result.returncode}\n"
            f"Stderr: {result.stderr}\n"
            f"Directory listing:\n{debug_result.stdout}"
        )

    settings_json_content = result.stdout

    # === Phase 3: Cleanup ===

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

    # === Phase 4: Assertions ===

    try:
        container_settings = json.loads(settings_json_content)
    except json.JSONDecodeError as e:
        raise AssertionError(
            f"Failed to parse JSON from container settings.json:\n{settings_json_content}\n\nError: {e}"
        )

    # Verify user settings are preserved
    assert "awsAuthRefresh" in container_settings, (
        f"User setting 'awsAuthRefresh' should be preserved. Got: {json.dumps(container_settings, indent=2)}"
    )

    assert container_settings["awsAuthRefresh"] == "aws sso login --profile bedrock-users", (
        f"User setting 'awsAuthRefresh' value should be preserved. Got: {container_settings['awsAuthRefresh']}"
    )

    assert "env" in container_settings, (
        f"User setting 'env' should be preserved. Got: {json.dumps(container_settings, indent=2)}"
    )

    assert "AWS_PROFILE" in container_settings["env"], (
        f"User env var 'AWS_PROFILE' should be preserved. Got env: {container_settings.get('env', {})}"
    )

    assert container_settings["env"]["AWS_PROFILE"] == "bedrock-users", (
        f"User env var 'AWS_PROFILE' value should be preserved. Got: {container_settings['env']['AWS_PROFILE']}"
    )

    assert "userCustomSetting" in container_settings, (
        f"User custom setting should be preserved. Got: {json.dumps(container_settings, indent=2)}"
    )

    # Verify sandbox settings are also present
    assert "allowDangerouslySkipPermissions" in container_settings, (
        f"Sandbox setting 'allowDangerouslySkipPermissions' should be added. Got: {json.dumps(container_settings, indent=2)}"
    )

    assert container_settings["allowDangerouslySkipPermissions"] is True, (
        f"Sandbox setting 'allowDangerouslySkipPermissions' should be True. Got: {container_settings['allowDangerouslySkipPermissions']}"
    )

    print("✓ All assertions passed: User settings preserved AND sandbox settings added")
