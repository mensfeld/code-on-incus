"""
Test for coi run - with single environment variable via config file.

Tests that:
1. Create .coi/config.toml with [defaults.environment] to set env var
2. Run command and verify env var is available in container
"""

import os
import subprocess
from pathlib import Path


def test_run_with_env(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running command with environment variables set via config file.

    Flow:
    1. Create .coi/config.toml with [defaults.environment] MY_TEST_VAR = "test-value-xyz"
    2. Run coi run -- sh -c 'echo $MY_TEST_VAR'
    3. Verify MY_TEST_VAR appears in output
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults.environment]
MY_TEST_VAR = "test-value-xyz"
"""
    )

    env = os.environ.copy()
    env["COI_CONFIG"] = str(config_dir / "config.toml")

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo $MY_TEST_VAR",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "test-value-xyz" in combined_output, (
        f"Output should contain env var value. Got:\n{combined_output}"
    )
