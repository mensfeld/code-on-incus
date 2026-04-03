"""
Test for UID mapping fix using raw.idmap instead of shift=true.

Validates that raw.idmap correctly maps the host user's UID to the container's
code user UID (1000), allowing full read/write access regardless of host UID.

This is the fix for the shift=true bug: raw.idmap explicitly maps
"both <hostUID> 1000", so the container's code user always sees host files
as its own.

Uses incus init (not launch) to create the container without starting it,
sets raw.idmap and security config before first boot — matching the
production code path in session/setup.go.

Tests that:
1. Create container without starting (incus init)
2. Enable Docker/nesting support (security flags)
3. Set raw.idmap to map host UID -> container UID 1000
4. Mount workspace WITHOUT shift
5. Start container
6. As code user (UID 1000), read, create, and overwrite files
7. All operations succeed regardless of host UID
"""

import os
import subprocess
import tempfile
import time

from support.helpers import (
    calculate_container_name,
)


def _incus_run(*args):
    """
    Run an incus command directly (no sg wrapper).

    Uses subprocess list args to avoid shell quoting issues.
    Includes --project default to match the coi binary's behavior.
    Works on CI (socket is chmod 666) and local dev (user in incus-admin group).
    """
    return subprocess.run(
        ["incus", "--project", "default", *args],
        capture_output=True,
        text=True,
        timeout=60,
    )


def test_workspace_write_access_raw_idmap(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that raw.idmap workspace mounts allow the code user to read/write files.

    Unlike shift=true, raw.idmap explicitly maps the host UID to container
    UID 1000, which works regardless of the host user's UID.

    This test mirrors the production code path: init → configure → mount → start
    (raw.idmap must be set before the container's first boot).

    Flow:
    1. incus init (create container without starting)
    2. Set security flags and raw.idmap
    3. Mount workspace WITHOUT shift
    4. Start container
    5. Exec as code user (UID 1000) to read, create, and overwrite files
    6. Assert all operations succeed
    7. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)
    host_uid = os.getuid()

    # === Phase 1: Create temp directory with a host-owned file ===

    with tempfile.TemporaryDirectory() as tmpdir:
        test_file = os.path.join(tmpdir, "host-file.txt")
        with open(test_file, "w") as f:
            f.write("written-by-host")

        # === Phase 2: Create container without starting ===

        result = _incus_run("init", "coi-default", container_name)
        assert result.returncode == 0, f"incus init should succeed. stderr: {result.stderr}"

        # === Phase 3: Configure security flags (same as EnableDockerSupport) ===

        for config in [
            ("security.nesting", "true"),
            ("security.syscalls.intercept.mknod", "true"),
            ("security.syscalls.intercept.setxattr", "true"),
            ("linux.sysctl.net.ipv4.ip_unprivileged_port_start", "0"),
        ]:
            result = _incus_run("config", "set", container_name, f"{config[0]}={config[1]}")
            assert result.returncode == 0, (
                f"Setting {config[0]} should succeed. stderr: {result.stderr}"
            )

        # === Phase 4: Set raw.idmap (must be before first boot) ===
        # Uses key/value as separate args to avoid any shell quoting issues

        idmap_value = f"both {host_uid} 1000"
        result = _incus_run("config", "set", container_name, "raw.idmap", idmap_value)
        assert result.returncode == 0, f"Setting raw.idmap should succeed. stderr: {result.stderr}"

        # Verify raw.idmap was set correctly
        result = _incus_run("config", "get", container_name, "raw.idmap")
        assert result.returncode == 0, f"Getting raw.idmap should succeed. stderr: {result.stderr}"
        assert idmap_value in result.stdout, (
            f"raw.idmap should be '{idmap_value}', got: '{result.stdout.strip()}'"
        )

        # === Phase 5: Mount workspace WITHOUT shift ===

        result = subprocess.run(
            [
                coi_binary,
                "container",
                "mount",
                container_name,
                "workspace",
                tmpdir,
                "/workspace",
                "--shift=false",
            ],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode == 0, f"Mount should succeed. stderr: {result.stderr}"

        # === Phase 6: Start container ===

        result = subprocess.run(
            [coi_binary, "container", "start", container_name],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode == 0, f"Container start should succeed. stderr: {result.stderr}"

        time.sleep(3)

        # === Phase 7: Test read/write as code user (UID 1000) ===

        # 7a. Read the host-created file
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
                "/workspace/host-file.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode == 0, (
            f"Code user should be able to read host file with raw.idmap "
            f"(host UID={host_uid} -> container UID=1000). stderr: {result.stderr}"
        )
        assert "written-by-host" in result.stdout + result.stderr, (
            f"Host file should contain expected content. Got: {result.stdout + result.stderr}"
        )

        # 7b. Create a new file
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
                "sh",
                "-c",
                "echo created-by-code > /workspace/code-file.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode == 0, (
            f"Code user should be able to create files with raw.idmap "
            f"(host UID={host_uid} -> container UID=1000). stderr: {result.stderr}"
        )

        # 7c. Overwrite the host-created file
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
                "sh",
                "-c",
                "echo overwritten-by-code > /workspace/host-file.txt",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )

        assert result.returncode == 0, (
            f"Code user should be able to overwrite host files with raw.idmap "
            f"(host UID={host_uid} -> container UID=1000). stderr: {result.stderr}"
        )

        # === Phase 8: Cleanup ===

        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
