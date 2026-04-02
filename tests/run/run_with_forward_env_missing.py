"""
Test for coi run - forward_env with unset host variable via config file.

Tests that:
1. forward_env in config with a variable NOT set on host produces a warning
2. The variable is NOT set in the container
3. The command still succeeds (unset vars are skipped, not fatal)
"""

import os
import subprocess
from pathlib import Path


def test_run_with_forward_env_missing(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that forwarding an unset host variable warns and skips.

    Flow:
    1. Ensure COI_NONEXISTENT_VAR is NOT set on host
    2. Create .coi/config.toml with forward_env = ["COI_NONEXISTENT_VAR"]
    3. Run coi run -- sh -c 'echo VAL=${COI_NONEXISTENT_VAR:-empty}'
    4. Verify warning appears in stderr
    5. Verify container sees the var as empty
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
forward_env = ["COI_NONEXISTENT_VAR"]
"""
    )

    env = os.environ.copy()
    env["COI_CONFIG"] = str(config_dir / "config.toml")
    env.pop("COI_NONEXISTENT_VAR", None)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo VAL=${COI_NONEXISTENT_VAR:-empty}",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    # Should warn about the missing variable
    assert "Warning" in result.stderr and "COI_NONEXISTENT_VAR" in result.stderr, (
        f"Should warn about unset forward_env var. stderr:\n{result.stderr}"
    )

    # Variable should not be set in container
    combined_output = result.stdout + result.stderr
    assert "VAL=empty" in combined_output, (
        f"Unset host var should not be forwarded. Got:\n{combined_output}"
    )
