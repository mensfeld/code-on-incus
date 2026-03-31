"""
Test that .coi.toml in project root triggers a hard deprecation error.

Tests that:
1. .coi.toml in project root → error with migration instructions
2. Error message includes the migration command
"""

import subprocess
from pathlib import Path


def test_coi_toml_deprecation_error(coi_binary, workspace_dir):
    """
    Test that .coi.toml triggers a hard error with migration message.

    Flow:
    1. Create .coi.toml in workspace
    2. Run coi run -- echo hello
    3. Verify it fails with migration error
    """
    config_path = Path(workspace_dir) / ".coi.toml"
    config_path.write_text(
        """
[defaults]
image = "coi"
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "echo",
            "hello",
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, f"Should fail when .coi.toml exists. stdout: {result.stdout}"

    combined_output = result.stdout + result.stderr
    assert (
        "found .coi.toml in project root" in combined_output.lower()
        or "found .coi.toml" in combined_output
    ), f"Error should mention deprecated .coi.toml. Got:\n{combined_output}"
    assert "mkdir -p .coi && mv .coi.toml .coi/config.toml" in combined_output, (
        f"Error should include migration command. Got:\n{combined_output}"
    )


def test_coi_toml_deprecation_even_with_coi_dir(coi_binary, workspace_dir):
    """
    Test that .coi.toml still triggers error even when .coi/config.toml also exists.

    Both files present → error for .coi.toml fires (no silent fallthrough).
    """
    # Create both files
    config_path = Path(workspace_dir) / ".coi.toml"
    config_path.write_text('[defaults]\nimage = "coi"\n')

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    new_config_path = config_dir / "config.toml"
    new_config_path.write_text('[defaults]\nimage = "coi"\n')

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "echo",
            "hello",
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, (
        "Should fail when .coi.toml exists even alongside .coi/config.toml"
    )

    combined_output = result.stdout + result.stderr
    assert "found .coi.toml" in combined_output.lower() or ".coi.toml" in combined_output, (
        f"Error should mention deprecated .coi.toml. Got:\n{combined_output}"
    )
