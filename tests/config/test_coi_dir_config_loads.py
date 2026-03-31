"""
Test that .coi/config.toml is loaded as project config.

Tests that:
1. Config placed in .coi/config.toml is loaded and applied
2. Environment variables from config are injected into the container
"""

import subprocess
from pathlib import Path


def test_coi_dir_config_loads(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that .coi/config.toml is loaded as project config.

    Flow:
    1. Create .coi/config.toml with environment = { COI_DIR_TEST = "works" }
    2. Run coi run -- sh -c 'echo $COI_DIR_TEST'
    3. Verify the env var is set
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        """
[defaults]
environment = { COI_DIR_TEST = "coi-dir-works-42" }
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
            "echo $COI_DIR_TEST",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "coi-dir-works-42" in combined_output, (
        f".coi/config.toml should be loaded. Got:\n{combined_output}"
    )
