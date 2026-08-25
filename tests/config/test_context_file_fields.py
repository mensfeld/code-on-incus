"""
Test that SANDBOX_CONTEXT.md contains the expected environment detail fields.

Verifies that:
1. The context file contains standard environment details (timezone, tool name, etc.)
2. The container name appears in the rendered context file.
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


def test_context_file_contains_environment_details(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """
    Test that ~/SANDBOX_CONTEXT.md contains expected environment detail fields
    including timezone, tool name, and container name.

    Flow:
    1. Start coi shell with dummy tool
    2. Wait for ready
    3. Read ~/SANDBOX_CONTEXT.md via incus exec
    4. Verify it contains key fields
    """
    env = {"COI_USE_DUMMY": "1"}
    slot = 1
    container_name = calculate_container_name(workspace_dir, slot)

    # Create fake home with credentials so setup runs
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    claude_dir = fake_home / ".claude"
    claude_dir.mkdir()
    credentials_file = claude_dir / ".credentials.json"
    credentials_file.write_text('{"token": "test"}')
    env["HOME"] = str(fake_home)

    # Start session
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

    # Read SANDBOX_CONTEXT.md from container
    result = subprocess.run(
        [
            "sg",
            "incus-admin",
            "-c",
            f"incus exec {container_name} -- cat /home/code/SANDBOX_CONTEXT.md",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    context_content = result.stdout
    context_exists = result.returncode == 0

    # Cleanup
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
    assert context_exists, "~/SANDBOX_CONTEXT.md should exist in container"

    assert "COI Sandbox Environment" in context_content, (
        f"Context file should contain header. Got:\n{context_content[:500]}"
    )

    assert "Environment Details" in context_content, (
        f"Context file should contain Environment Details table. Got:\n{context_content[:500]}"
    )

    # Default timezone should be UTC
    assert "UTC" in context_content, (
        f"Context file should contain default timezone UTC. Got:\n{context_content[:500]}"
    )

    # Tool name should be present (claude is the default tool)
    assert "claude" in context_content, (
        f"Context file should contain tool name 'claude'. Got:\n{context_content[:500]}"
    )


def test_context_file_contains_container_name(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """
    Test that ~/SANDBOX_CONTEXT.md contains the actual container name.

    Flow:
    1. Start coi shell with dummy tool at a specific slot
    2. Wait for ready
    3. Read ~/SANDBOX_CONTEXT.md via incus exec
    4. Verify the calculated container name appears in the file
    """
    env = {"COI_USE_DUMMY": "1"}
    slot = 1
    container_name = calculate_container_name(workspace_dir, slot)

    # Create fake home with credentials
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    claude_dir = fake_home / ".claude"
    claude_dir.mkdir()
    credentials_file = claude_dir / ".credentials.json"
    credentials_file.write_text('{"token": "test"}')
    env["HOME"] = str(fake_home)

    # Start session
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

    # Read SANDBOX_CONTEXT.md from container
    result = subprocess.run(
        [
            "sg",
            "incus-admin",
            "-c",
            f"incus exec {container_name} -- cat /home/code/SANDBOX_CONTEXT.md",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )

    context_content = result.stdout

    # Cleanup
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

    # The container name should appear in the context file
    assert container_name in context_content, (
        f"Context file should contain container name '{container_name}'. "
        f"Got:\n{context_content[:500]}"
    )


def test_context_json_file_contains_required_fields(
    coi_binary, cleanup_containers, workspace_dir, tmp_path
):
    """
    End-to-end: a real `coi shell` session must write ~/SANDBOX_CONTEXT.json next
    to the .md, and it must be valid JSON carrying the required structured fields
    (schema_version, container_name, tool_name, workspace_path, os, network.mode)
    for programmatic consumers (#705).

    Flow:
    1. Start coi shell with the dummy tool.
    2. Wait for ready, drop to bash.
    3. Read ~/SANDBOX_CONTEXT.json via incus exec and json.loads it.
    4. Assert the required fields are present and correct.
    """
    env = {"COI_USE_DUMMY": "1"}
    slot = 1
    container_name = calculate_container_name(workspace_dir, slot)

    # Create fake home with credentials so setup runs
    fake_home = tmp_path / "fake_home"
    fake_home.mkdir()
    claude_dir = fake_home / ".claude"
    claude_dir.mkdir()
    (claude_dir / ".credentials.json").write_text('{"token": "test"}')
    env["HOME"] = str(fake_home)

    child = spawn_coi(coi_binary, ["shell"], cwd=workspace_dir, env=env, timeout=120)

    wait_for_container_ready(child, timeout=60)
    wait_for_prompt(child, timeout=90)

    # Exit CLI to bash
    child.send("exit")
    time.sleep(0.3)
    child.send("\x0d")
    time.sleep(2)

    # Read SANDBOX_CONTEXT.json from the container
    result = subprocess.run(
        [
            "sg",
            "incus-admin",
            "-c",
            f"incus exec {container_name} -- cat /home/code/SANDBOX_CONTEXT.json",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    json_exists = result.returncode == 0
    json_content = result.stdout

    # Cleanup
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
    assert json_exists, "~/SANDBOX_CONTEXT.json should exist in container"

    try:
        data = json.loads(json_content)
    except json.JSONDecodeError as e:
        raise AssertionError(
            f"~/SANDBOX_CONTEXT.json is not valid JSON: {e}\nGot:\n{json_content[:500]}"
        ) from e

    assert data.get("schema_version") == 1, (
        f"schema_version should be 1, got {data.get('schema_version')!r}"
    )
    assert data.get("container_name") == container_name, (
        f"container_name should be {container_name!r}, got {data.get('container_name')!r}"
    )
    assert data.get("tool_name"), f"tool_name should be non-empty, got {data.get('tool_name')!r}"
    assert data.get("workspace_path"), (
        f"workspace_path should be non-empty, got {data.get('workspace_path')!r}"
    )
    # os is defaulted server-side, so it must never be empty.
    assert data.get("os"), f"os should be non-empty (defaulted), got {data.get('os')!r}"
    assert isinstance(data.get("network"), dict) and "mode" in data["network"], (
        f"network.mode should be present, got network={data.get('network')!r}"
    )
