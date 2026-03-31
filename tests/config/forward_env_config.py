"""
Test that forward_env from .coi/config.toml config file works.

Tests that:
1. forward_env in [defaults] section forwards host env vars into container
2. Values are resolved from the host, not stored in config
"""

import os
import subprocess
from pathlib import Path


def test_forward_env_from_config(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that forward_env in .coi/config.toml forwards host env vars.

    Flow:
    1. Create .coi/config.toml with forward_env = ["COI_CFG_SECRET"]
    2. Set COI_CFG_SECRET on host
    3. Run coi run -- sh -c 'echo $COI_CFG_SECRET'
    4. Verify the value is forwarded
    """
    # Create project config with forward_env
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        """
[defaults]
forward_env = ["COI_CFG_SECRET"]
"""
    )

    env = os.environ.copy()
    env["COI_CFG_SECRET"] = "secret-from-config-99"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo $COI_CFG_SECRET",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "secret-from-config-99" in combined_output, (
        f"Output should contain forwarded env var value from config. Got:\n{combined_output}"
    )


def test_forward_env_config_and_flag_merge(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that forward_env from config and --forward-env flag are merged.

    Flow:
    1. Create .coi/config.toml with forward_env = ["COI_FROM_CFG"]
    2. Run with --forward-env COI_FROM_FLAG
    3. Both should be forwarded
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        """
[defaults]
forward_env = ["COI_FROM_CFG"]
"""
    )

    env = os.environ.copy()
    env["COI_FROM_CFG"] = "cfg-value"
    env["COI_FROM_FLAG"] = "flag-value"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--forward-env",
            "COI_FROM_FLAG",
            "--",
            "sh",
            "-c",
            "echo CFG=$COI_FROM_CFG FLAG=$COI_FROM_FLAG",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "CFG=cfg-value" in combined_output, (
        f"Config forward_env should be forwarded. Got:\n{combined_output}"
    )
    assert "FLAG=flag-value" in combined_output, (
        f"Flag forward_env should be forwarded. Got:\n{combined_output}"
    )
