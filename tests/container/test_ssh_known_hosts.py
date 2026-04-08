"""
Test that SSH known_hosts is pre-populated for common Git hosts.

Tests that:
1. Launch a container with coi-default image
2. Verify /home/code/.ssh/known_hosts exists and is non-empty
3. Verify it contains entries for github.com, gitlab.com, bitbucket.org
4. Verify file permissions (644) and directory permissions (700)
5. Verify accessible as code user (UID 1000, GID 1000)
"""

import subprocess
import time

from support.helpers import calculate_container_name


def test_ssh_known_hosts(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that the coi image has SSH known_hosts pre-populated for common Git hosts.

    Flow:
    1. Launch a container
    2. Verify known_hosts exists and is non-empty
    3. Verify entries for github.com, gitlab.com, bitbucket.org
    4. Verify file and directory permissions
    5. Verify accessible as code user
    6. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 2: Verify known_hosts exists and is non-empty ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "test",
            "-s",
            "/home/code/.ssh/known_hosts",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, (
        "known_hosts should exist and be non-empty. stderr: " + result.stderr
    )

    # === Phase 3: Verify entries for common Git hosts ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "cat",
            "/home/code/.ssh/known_hosts",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Should be able to read known_hosts. stderr: {result.stderr}"

    known_hosts = result.stdout + result.stderr
    for host in ("github.com", "gitlab.com", "bitbucket.org"):
        assert host in known_hosts, (
            f"known_hosts should contain entry for {host}. Content: {known_hosts}"
        )

    # === Phase 4: Verify file permissions (644) and directory permissions (700) ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "stat",
            "-c",
            "%a",
            "/home/code/.ssh/known_hosts",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"stat on known_hosts should succeed. stderr: {result.stderr}"
    perms = (result.stdout + result.stderr).strip()
    assert perms == "644", f"known_hosts should have 644 permissions. Got: {perms}"

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "stat",
            "-c",
            "%a",
            "/home/code/.ssh",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"stat on .ssh should succeed. stderr: {result.stderr}"
    perms = (result.stdout + result.stderr).strip()
    assert perms == "700", f".ssh directory should have 700 permissions. Got: {perms}"

    # === Phase 5: Verify accessible as code user (UID 1000, GID 1000) ===

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--user",
            "1000",
            "--group",
            "1000",
            "--",
            "cat",
            "/home/code/.ssh/known_hosts",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, (
        "code user (UID 1000) should be able to read known_hosts. stderr: " + result.stderr
    )
    user_known_hosts = result.stdout + result.stderr
    assert "github.com" in user_known_hosts, (
        f"code user should see github.com in known_hosts. Content: {user_known_hosts}"
    )

    # === Phase 6: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
