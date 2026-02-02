"""
Critical snapshot scenarios - real-world use cases and edge cases.

Tests that:
1. Workspace files are NOT affected by snapshots (workspace is mounted, not in container)
2. Session data (.claude/) is properly restored from snapshots
3. Multiple snapshot branching works (restore to earlier snapshot)
4. File permissions are preserved across snapshot restore
"""

import json
import subprocess
import time

from support.helpers import calculate_container_name


def test_workspace_files_not_in_snapshot(coi_binary, cleanup_containers, workspace_dir):
    """
    CRITICAL: Verify that workspace files are NOT included in snapshots.

    Workspace is mounted into container, not part of container filesystem.
    Changes to /workspace should persist independently of snapshot restore.

    Flow:
    1. Launch container with workspace mounted
    2. Create file in /workspace
    3. Create snapshot
    4. Delete file from /workspace
    5. Restore snapshot
    6. Verify file is STILL GONE (workspace not affected by snapshot)
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "workspace-test"

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Mount workspace into container ===
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "mount",
            container_name,
            "workspace",
            workspace_dir,
            "/workspace",
            "--shift",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Workspace mount should succeed. stderr: {result.stderr}"

    # === Phase 3: Create file in /workspace (mounted directory) ===
    workspace_file = "/workspace/test-workspace-file.txt"
    workspace_content = "This file is in mounted workspace"

    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            f"echo '{workspace_content}' > {workspace_file}",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, (
        f"Creating workspace file should succeed. stderr: {result.stderr}"
    )

    # Verify file exists in workspace
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--capture", "--", "cat", workspace_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Workspace file should exist"
    output = json.loads(result.stdout)
    assert workspace_content in output["stdout"], "Workspace file should have content"

    # === Phase 4: Create snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot create should succeed. stderr: {result.stderr}"

    # === Phase 5: Delete file from /workspace ===
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "rm", workspace_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, (
        f"Deleting workspace file should succeed. stderr: {result.stderr}"
    )

    # Verify file is gone
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ls", workspace_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "Workspace file should be deleted"

    # === Phase 6: Stop and restore snapshot ===
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    result = subprocess.run(
        [coi_binary, "snapshot", "restore", snapshot_name, "-c", container_name, "-f"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot restore should succeed. stderr: {result.stderr}"

    result = subprocess.run(
        [coi_binary, "container", "start", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container start should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 7: Verify workspace file is STILL GONE ===
    # This proves workspace is NOT part of snapshot
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ls", workspace_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, (
        "Workspace file should still be gone - workspace not included in snapshot"
    )

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_session_data_restoration(coi_binary, cleanup_containers, workspace_dir):
    """
    CRITICAL: Verify that session data (.claude/) is properly restored.

    This is the core use case for snapshots - preserving AI assistant session state.

    Flow:
    1. Launch container
    2. Create session data in /root/.claude/
    3. Create snapshot
    4. Delete .claude directory
    5. Restore snapshot
    6. Verify session data is restored
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "session-data-test"
    session_file = "/root/.claude/session.json"
    session_data = '{"session_id": "test-123", "messages": ["hello", "world"]}'

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create session data ===
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            f"mkdir -p /root/.claude && echo '{session_data}' > {session_file}",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Creating session data should succeed. stderr: {result.stderr}"

    # Verify session file exists with correct data
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--capture", "--", "cat", session_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Session file should exist"
    output = json.loads(result.stdout)
    assert "test-123" in output["stdout"], "Session file should have correct data"

    # === Phase 3: Create snapshot (with session data) ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot create should succeed. stderr: {result.stderr}"

    # === Phase 4: Delete entire .claude directory ===
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "rm", "-rf", "/root/.claude"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Deleting .claude should succeed. stderr: {result.stderr}"

    # Verify directory is gone
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ls", "/root/.claude"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, ".claude directory should be deleted"

    # === Phase 5: Stop and restore snapshot ===
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container stop should succeed. stderr: {result.stderr}"

    result = subprocess.run(
        [coi_binary, "snapshot", "restore", snapshot_name, "-c", container_name, "-f"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Snapshot restore should succeed. stderr: {result.stderr}"

    result = subprocess.run(
        [coi_binary, "container", "start", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Container start should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 6: Verify session data is fully restored ===
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--capture", "--", "cat", session_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Session file should be restored"
    output = json.loads(result.stdout)
    assert "test-123" in output["stdout"], "Session data should be restored with original content"
    assert "hello" in output["stdout"], "Session messages should be restored"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_multiple_snapshot_branching(coi_binary, cleanup_containers, workspace_dir):
    """
    Test creating multiple snapshots and restoring to an earlier state.

    This tests the branching workflow - experiment with different approaches
    and roll back to earlier checkpoints.

    Note: ZFS doesn't allow restoring to an earlier snapshot if subsequent
    snapshots exist. Must delete later snapshots first.

    Flow:
    1. Create file A, snapshot "state1"
    2. Create file B, snapshot "state2"
    3. Create file C, snapshot "state3"
    4. Delete state2 and state3 (ZFS requirement)
    5. Restore to "state1"
    6. Verify only file A exists (B and C are gone)
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create file A and snapshot state1 ===
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            "echo 'File A' > /root/fileA.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Creating file A should succeed"

    result = subprocess.run(
        [coi_binary, "snapshot", "create", "state1", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Snapshot state1 should succeed"

    # === Phase 3: Create file B and snapshot state2 ===
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            "echo 'File B' > /root/fileB.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Creating file B should succeed"

    result = subprocess.run(
        [coi_binary, "snapshot", "create", "state2", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Snapshot state2 should succeed"

    # === Phase 4: Create file C and snapshot state3 ===
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            "echo 'File C' > /root/fileC.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Creating file C should succeed"

    result = subprocess.run(
        [coi_binary, "snapshot", "create", "state3", "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Snapshot state3 should succeed"

    # Verify all three files exist
    for file in ["fileA.txt", "fileB.txt", "fileC.txt"]:
        result = subprocess.run(
            [coi_binary, "container", "exec", container_name, "--", "ls", f"/root/{file}"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        assert result.returncode == 0, f"{file} should exist before restore"

    # === Phase 5: Delete later snapshots (ZFS requirement) ===
    # ZFS doesn't allow restoring to earlier snapshot if subsequent snapshots exist
    # Delete state2 and state3 before restoring to state1
    for snap in ["state3", "state2"]:
        result = subprocess.run(
            [coi_binary, "snapshot", "delete", snap, "-c", container_name],
            capture_output=True,
            text=True,
            timeout=60,
        )
        assert result.returncode == 0, f"Deleting {snap} should succeed"

    # === Phase 6: Restore to state1 (only file A should exist) ===
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Container stop should succeed"

    result = subprocess.run(
        [coi_binary, "snapshot", "restore", "state1", "-c", container_name, "-f"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Restore to state1 should succeed"

    result = subprocess.run(
        [coi_binary, "container", "start", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Container start should succeed"
    time.sleep(3)

    # === Phase 7: Verify only file A exists ===
    # File A should exist
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--capture",
            "--",
            "cat",
            "/root/fileA.txt",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "File A should exist after restore to state1"
    output = json.loads(result.stdout)
    assert "File A" in output["stdout"], "File A should have original content"

    # Files B and C should NOT exist
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ls", "/root/fileB.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "File B should not exist after restore to state1"

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "ls", "/root/fileC.txt"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode != 0, "File C should not exist after restore to state1"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )


def test_file_permissions_preserved(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that file permissions are preserved across snapshot restore.

    Flow:
    1. Create file with specific permissions (chmod 600)
    2. Snapshot
    3. Change permissions (chmod 777)
    4. Restore
    5. Verify permissions are back to 600
    """
    container_name = calculate_container_name(workspace_dir, 1)
    snapshot_name = "permissions-test"
    test_file = "/root/permissions-test.txt"

    # === Phase 1: Launch container ===
    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    # === Phase 2: Create file with chmod 600 ===
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--",
            "bash",
            "-c",
            f"echo 'secret' > {test_file} && chmod 600 {test_file}",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Creating file with permissions should succeed"

    # Verify permissions are 600
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--capture",
            "--",
            "stat",
            "-c",
            "%a",
            test_file,
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Checking permissions should succeed"
    output = json.loads(result.stdout)
    assert "600" in output["stdout"], "File should have 600 permissions"

    # === Phase 3: Create snapshot ===
    result = subprocess.run(
        [coi_binary, "snapshot", "create", snapshot_name, "-c", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Snapshot create should succeed"

    # === Phase 4: Change permissions to 777 ===
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "chmod", "777", test_file],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Changing permissions should succeed"

    # Verify permissions are now 777
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--capture",
            "--",
            "stat",
            "-c",
            "%a",
            test_file,
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Checking permissions should succeed"
    output = json.loads(result.stdout)
    assert "777" in output["stdout"], "File should have 777 permissions before restore"

    # === Phase 5: Restore snapshot ===
    result = subprocess.run(
        [coi_binary, "container", "stop", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Container stop should succeed"

    result = subprocess.run(
        [coi_binary, "snapshot", "restore", snapshot_name, "-c", container_name, "-f"],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Snapshot restore should succeed"

    result = subprocess.run(
        [coi_binary, "container", "start", container_name],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, "Container start should succeed"
    time.sleep(3)

    # === Phase 6: Verify permissions restored to 600 ===
    result = subprocess.run(
        [
            coi_binary,
            "container",
            "exec",
            container_name,
            "--capture",
            "--",
            "stat",
            "-c",
            "%a",
            test_file,
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, "Checking permissions should succeed"
    output = json.loads(result.stdout)
    assert "600" in output["stdout"], "File permissions should be restored to 600"

    # === Cleanup ===
    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
