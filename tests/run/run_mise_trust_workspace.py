"""
Test that MISE_TRUSTED_CONFIG_PATHS is set to the workspace path in container shells.

The env var is written to both /etc/profile.d/coi-mise-trust.sh (login shells)
and /etc/bash.bashrc (non-login interactive shells) during session setup,
so mise automatically trusts config files in the workspace.
"""

import subprocess


def test_mise_trust_env_login_shell(coi_binary, cleanup_containers, workspace_dir):
    """
    Test MISE_TRUSTED_CONFIG_PATHS is set in a login shell.

    Login shells source /etc/profile.d/*.sh, where coi-mise-trust.sh sets the var.
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "bash",
            "-l",
            "-c",
            "echo TRUST_PATH=$MISE_TRUSTED_CONFIG_PATHS",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined = result.stdout + result.stderr
    assert "TRUST_PATH=/workspace" in combined, (
        f"MISE_TRUSTED_CONFIG_PATHS should be /workspace in login shell. Got:\n{combined}"
    )


def test_mise_trust_env_nonlogin_shell(coi_binary, cleanup_containers, workspace_dir):
    """
    Test MISE_TRUSTED_CONFIG_PATHS is set in a non-login interactive shell.

    Non-login shells source /etc/bash.bashrc (but NOT /etc/profile.d/),
    where coi also injects the var so it's set before ~/.bashrc activates mise.
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "bash",
            "-c",
            # Source /etc/bash.bashrc explicitly since bash -c is non-interactive
            # and won't source it automatically. This mirrors what happens in
            # interactive non-login shells (like tmux bash).
            ". /etc/bash.bashrc 2>/dev/null; echo TRUST_PATH=$MISE_TRUSTED_CONFIG_PATHS",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined = result.stdout + result.stderr
    assert "TRUST_PATH=/workspace" in combined, (
        f"MISE_TRUSTED_CONFIG_PATHS should be /workspace via bash.bashrc. Got:\n{combined}"
    )


def test_mise_trust_profile_d_file_exists(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that /etc/profile.d/coi-mise-trust.sh is created with correct content.
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "cat",
            "/etc/profile.d/coi-mise-trust.sh",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"File should exist. stderr: {result.stderr}"

    combined = result.stdout + result.stderr
    assert 'MISE_TRUSTED_CONFIG_PATHS="/workspace"' in combined, (
        f"Profile.d file should contain the workspace path. Got:\n{combined}"
    )
