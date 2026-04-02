"""
Test for coi run - with forward_env via config file.

Tests that:
1. Create .coi/config.toml with forward_env to forward a host env var by name
2. Verify env var value from host is available in container
"""

import os
import subprocess
from pathlib import Path


def test_run_with_forward_env(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running command with forward_env set via config file.

    Flow:
    1. Set a host env var
    2. Create .coi/config.toml with forward_env = ["COI_TEST_FORWARD_VAR"]
    3. Run coi run -- sh -c 'echo $COI_TEST_FORWARD_VAR'
    4. Verify the host value appears in container output
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
forward_env = ["COI_TEST_FORWARD_VAR"]
"""
    )

    env = os.environ.copy()
    env["COI_CONFIG"] = str(config_dir / "config.toml")
    env["COI_TEST_FORWARD_VAR"] = "forwarded-value-42"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo $COI_TEST_FORWARD_VAR",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "forwarded-value-42" in combined_output, (
        f"Output should contain forwarded env var value. Got:\n{combined_output}"
    )
