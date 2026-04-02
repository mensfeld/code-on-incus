"""
Test for coi run - with multiple forward_env variables via config file.

Tests that:
1. Create .coi/config.toml with forward_env forwarding multiple host env vars
2. Verify all forwarded values are available in container
"""

import os
import subprocess
from pathlib import Path


def test_run_with_forward_env_multiple(coi_binary, cleanup_containers, workspace_dir):
    """
    Test running command with multiple forwarded env vars via config file.

    Flow:
    1. Set multiple host env vars
    2. Create .coi/config.toml with forward_env = ["COI_FWD_A", "COI_FWD_B"]
    3. Run coi run -- sh -c 'echo $COI_FWD_A $COI_FWD_B'
    4. Verify both values appear in container output
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
forward_env = ["COI_FWD_A", "COI_FWD_B"]
"""
    )

    env = os.environ.copy()
    env["COI_CONFIG"] = str(config_dir / "config.toml")
    env["COI_FWD_A"] = "alpha-val"
    env["COI_FWD_B"] = "beta-val"

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo $COI_FWD_A $COI_FWD_B",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "alpha-val" in combined_output, (
        f"Output should contain COI_FWD_A value. Got:\n{combined_output}"
    )
    assert "beta-val" in combined_output, (
        f"Output should contain COI_FWD_B value. Got:\n{combined_output}"
    )
