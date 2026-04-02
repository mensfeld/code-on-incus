"""
Test that [defaults] environment variables from .coi/config.toml are applied.

Tests that:
1. Static env vars in [defaults.environment] are set in the container
2. Multiple env vars in [defaults.environment] are all applied
"""

import subprocess
from pathlib import Path


def test_defaults_environment_applied(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that [defaults] environment vars are injected into container.

    Flow:
    1. Create .coi/config.toml with [defaults.environment] MY_DEFAULT = "default-val-55"
    2. Run coi run -- sh -c 'echo $MY_DEFAULT'
    3. Verify MY_DEFAULT is set
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        """
[defaults.environment]
MY_DEFAULT = "default-val-55"
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


def test_defaults_environment_multiple_vars(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that multiple [defaults.environment] vars are all applied.

    Flow:
    1. Create .coi/config.toml with [defaults.environment] containing two vars
    2. Run coi run and check both are set
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_path = config_dir / "config.toml"
    config_path.write_text(
        """
[defaults.environment]
MY_VAR = "from-config"
MY_OTHER_VAR = "also-from-config"
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
            "echo V1=$MY_VAR V2=$MY_OTHER_VAR",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "V1=from-config" in combined_output, (
        f"MY_VAR should be set from defaults.environment. Got:\n{combined_output}"
    )
    assert "V2=also-from-config" in combined_output, (
        f"MY_OTHER_VAR should be set from defaults.environment. Got:\n{combined_output}"
    )
