"""
Test that container user UID/GID is remapped when code_uid differs from image default.

Regression test for GitHub issue #166: When code_uid is set to a non-default value
(e.g., 1001), the container user must be remapped so that /etc/passwd, home directory
ownership, and file permissions all match the configured UID.

Without the fix, the container has:
- "I have no name!" prompt (no user with UID 1001 in /etc/passwd)
- "Permission denied" on /home/code/.bashrc (owned by UID 1000)
- "cannot find name for group ID 1001"

Tests use `coi run` (full session setup → exec → cleanup) to exercise the
remap logic in internal/session/setup.go.
"""

import os
import subprocess


def test_custom_code_uid_user_remapped(coi_binary, cleanup_containers, workspace_dir):
    """
    With code_uid = 1001, verify `id code` shows uid=1001(code) gid=1001(code).
    """
    config_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(config_dir, exist_ok=True)
    config_path = os.path.join(config_dir, "config.toml")
    with open(config_path, "w") as f:
        f.write(
            """
[incus]
code_uid = 1001
"""
        )

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "id", "code"],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "uid=1001(code)" in combined_output, (
        f"User 'code' should have UID 1001. Got:\n{combined_output}"
    )
    assert "gid=1001(code)" in combined_output, (
        f"User 'code' should have GID 1001. Got:\n{combined_output}"
    )


def test_custom_code_uid_whoami(coi_binary, cleanup_containers, workspace_dir):
    """
    With code_uid = 1001, verify `whoami` returns 'code' (not 'I have no name!').
    """
    config_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(config_dir, exist_ok=True)
    config_path = os.path.join(config_dir, "config.toml")
    with open(config_path, "w") as f:
        f.write(
            """
[incus]
code_uid = 1001
"""
        )

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "whoami"],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "code" in combined_output, (
        f"whoami should return 'code', not 'I have no name!'. Got:\n{combined_output}"
    )


def test_custom_code_uid_home_ownership(coi_binary, cleanup_containers, workspace_dir):
    """
    With code_uid = 1001, verify /home/code is owned by UID 1001.
    """
    config_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(config_dir, exist_ok=True)
    config_path = os.path.join(config_dir, "config.toml")
    with open(config_path, "w") as f:
        f.write(
            """
[incus]
code_uid = 1001
"""
        )

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "stat", "-c", "%u", "/home/code"],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "1001" in combined_output, (
        f"/home/code should be owned by UID 1001. Got:\n{combined_output}"
    )


def test_custom_code_uid_bashrc_readable(coi_binary, cleanup_containers, workspace_dir):
    """
    With code_uid = 1001, verify .bashrc is readable (not 'Permission denied').
    """
    config_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(config_dir, exist_ok=True)
    config_path = os.path.join(config_dir, "config.toml")
    with open(config_path, "w") as f:
        f.write(
            """
[incus]
code_uid = 1001
"""
        )

    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "cat", "/home/code/.bashrc"],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "Permission denied" not in combined_output, (
        f".bashrc should be readable. Got:\n{combined_output}"
    )
