"""
Test that incus config values from .coi/config.toml are wired into command execution.

Regression test for: hardcoded constants in the container package ignoring
user-configured incus.code_uid, incus.code_user, incus.group, and incus.project.

Tests that:
1. Custom code_uid from .coi/config.toml is used when executing commands in containers
2. Default code_uid (1000) is used when no config override is present
"""

import os
import subprocess


def test_custom_code_uid_from_config(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that incus.code_uid from .coi/config.toml is applied to container exec.

    Flow:
    1. Create .coi/config.toml with code_uid = 1001
    2. Run coi run "id -u"
    3. Verify UID is 1001 (not the default 1000)
    """
    # Create .coi/config.toml with custom code_uid
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

    # Run from workspace_dir so config.Load() finds the .coi/config.toml
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "id", "-u"],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "1001" in combined_output, (
        f"Should run with UID 1001 from config. Got:\n{combined_output}"
    )


def test_default_code_uid_without_config(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that default code_uid (1000) is used when no config override is present.

    Flow:
    1. Run coi run "id -u" without any .coi/config.toml
    2. Verify UID is 1000 (the default)
    """
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "--", "id", "-u"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "1000" in combined_output, f"Should run with default UID 1000. Got:\n{combined_output}"
