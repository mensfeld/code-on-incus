"""
Test that [defaults] environment variables from .coi/config.toml are applied.

Tests that:
1. Static env vars in [defaults] environment are set in the container
2. --env flag takes precedence over defaults.environment
"""

import subprocess
from pathlib import Path


def test_defaults_environment_applied(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that [defaults] environment vars are injected into container.

    Flow:
    1. Create .coi/config.toml with [defaults] environment = { MY_DEFAULT = "default-val" }
    2. Run coi run -- sh -c 'echo $MY_DEFAULT'
    3. Verify MY_DEFAULT is set
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        """
[defaults]
environment = { MY_DEFAULT = "default-val-55" }
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo $MY_DEFAULT",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "default-val-55" in combined_output, (
        f"defaults.environment should be set in container. Got:\n{combined_output}"
    )


def test_env_flag_overrides_defaults_environment(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that --env flag takes precedence over defaults.environment.

    Flow:
    1. Create .coi/config.toml with [defaults] environment = { MY_VAR = "from-config" }
    2. Run with -e MY_VAR=from-flag
    3. Verify the flag value wins
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        """
[defaults]
environment = { MY_VAR = "from-config" }
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "-e",
            "MY_VAR=from-flag",
            "--",
            "sh",
            "-c",
            "echo $MY_VAR",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "from-flag" in combined_output, (
        f"--env flag should override defaults.environment. Got:\n{combined_output}"
    )
