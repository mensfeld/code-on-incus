"""
Test for coi run - with multiple environment variables via config file.

Tests that:
1. Create .coi/config.toml with multiple [defaults.environment] entries
2. Verify all env vars are set
"""

import os
import subprocess
from pathlib import Path


def test_run_with_multiple_env(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running command with multiple environment variables set via config file.

    Flow:
    1. Create .coi/config.toml with [defaults.environment] VAR1 and VAR2
    2. Run coi run -- sh -c 'echo $VAR1 $VAR2'
    3. Verify all env vars are set
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults.environment]
VAR1 = "value1"
VAR2 = "value2"
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
            "echo $VAR1 $VAR2",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "value1" in combined_output, f"Output should contain VAR1 value. Got:\n{combined_output}"
    assert "value2" in combined_output, f"Output should contain VAR2 value. Got:\n{combined_output}"
