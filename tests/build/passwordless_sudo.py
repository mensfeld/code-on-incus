"""Test that the code user has passwordless sudo access.

This test verifies that:
1. The code user can run sudo commands without a password
2. The sudoers configuration is correct
3. This critical security configuration is never accidentally removed

Why this matters:
- Passwordless sudo is required for the development workflow
- Users need to install packages, run docker, etc. without friction
- Breaking this would severely impact the user experience
"""

from support.helpers import run, snapshot_stdout


def test_passwordless_sudo_works(coi_binary, test_workspace, test_image):
    """Verify code user can run sudo commands without password."""
    # Start a shell and try to run a sudo command
    # This should succeed without prompting for a password
    result = run(
        coi_binary,
        "run",
        "--workspace",
        test_workspace,
        "--image",
        test_image,
        "--",
        "sudo",
        "id",
        "-u",
    )

    # Should show root uid (0)
    assert result.returncode == 0, "sudo command should succeed without password"
    assert "0" in result.stdout, "sudo should execute as root (uid 0)"


def test_sudoers_file_exists(coi_binary, test_workspace, test_image):
    """Verify the sudoers.d/code file exists with correct configuration."""
    result = run(
        coi_binary,
        "run",
        "--workspace",
        test_workspace,
        "--image",
        test_image,
        "--",
        "cat",
        "/etc/sudoers.d/code",
    )

    assert result.returncode == 0, "sudoers.d/code file should exist"
    assert "NOPASSWD:ALL" in result.stdout, "Should have NOPASSWD:ALL configuration"
    assert "code ALL=(ALL)" in result.stdout, "Should allow code user all commands"


def test_sudo_no_password_prompt(coi_binary, test_workspace, test_image):
    """Verify sudo doesn't prompt for password (non-interactive test)."""
    # Run sudo with -n flag (non-interactive)
    # This will fail if password is required
    result = run(
        coi_binary,
        "run",
        "--workspace",
        test_workspace,
        "--image",
        test_image,
        "--",
        "sudo",
        "-n",
        "whoami",
    )

    assert result.returncode == 0, "sudo -n should succeed (no password required)"
    assert "root" in result.stdout, "Should execute as root"


def test_code_user_in_sudo_group(coi_binary, test_workspace, test_image):
    """Verify code user is in the sudo group."""
    result = run(
        coi_binary,
        "run",
        "--workspace",
        test_workspace,
        "--image",
        test_image,
        "--",
        "groups",
        "code",
    )

    assert result.returncode == 0, "groups command should succeed"
    assert "sudo" in result.stdout, "code user should be in sudo group"


def test_sudoers_file_permissions(coi_binary, test_workspace, test_image):
    """Verify sudoers file has correct permissions (440)."""
    result = run(
        coi_binary,
        "run",
        "--workspace",
        test_workspace,
        "--image",
        test_image,
        "--",
        "stat",
        "-c",
        "%a",
        "/etc/sudoers.d/code",
    )

    assert result.returncode == 0, "stat command should succeed"
    assert "440" in result.stdout, "sudoers.d/code should have 440 permissions"
