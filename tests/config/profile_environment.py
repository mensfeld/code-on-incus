"""
Test that profile environment variables are applied to container.

Tests that:
1. Profile environment map is applied when --profile is used
2. Profile env vars are available inside the container
3. Profile environment overrides defaults environment
"""

import subprocess
from pathlib import Path


def test_profile_environment_applied(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that profile environment vars are injected into container.

    Flow:
    1. Create .coi/profiles/rust/config.toml with environment
    2. Run coi run --profile rust -- sh -c 'echo $RUST_BACKTRACE'
    3. Verify RUST_BACKTRACE is set to "full"
    """
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "rust"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[environment]
RUST_BACKTRACE = "full"
MY_PROFILE_VAR = "profile-val-77"
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "rust",
            "--",
            "sh",
            "-c",
            "echo BT=$RUST_BACKTRACE PV=$MY_PROFILE_VAR",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert "BT=full" in combined_output, (
        f"RUST_BACKTRACE should be set from profile. Got:\n{combined_output}"
    )
    assert "PV=profile-val-77" in combined_output, (
        f"MY_PROFILE_VAR should be set from profile. Got:\n{combined_output}"
    )


def test_profile_environment_overrides_defaults(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that profile environment takes precedence over defaults environment.

    Flow:
    1. Create .coi/config.toml with [defaults.environment] MY_VAR = "from-defaults"
    2. Create profile with [environment] MY_VAR = "from-profile"
    3. Run with --profile and verify profile value wins
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults.environment]
MY_VAR = "from-defaults"
"""
    )

    profile_dir = config_dir / "profiles" / "test"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(
        """
[environment]
MY_VAR = "from-profile"
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--profile",
            "test",
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
    assert "from-profile" in combined_output, (
        f"Profile environment should override defaults environment. Got:\n{combined_output}"
    )
