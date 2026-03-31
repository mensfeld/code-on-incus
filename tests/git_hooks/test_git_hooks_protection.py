"""Test git hooks security protection feature.

This feature mounts .git/hooks as read-only by default to protect against
malicious code injection that could execute on the host during git operations.
"""

import subprocess
from pathlib import Path


def test_git_hooks_readonly_by_default(coi_binary, workspace_dir, cleanup_containers):
    """Test that .git/hooks is mounted read-only by default in git repositories."""
    # Initialize a git repository in the workspace
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create hooks directory (git init may not create it)
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Run a command in the container that tries to write to .git/hooks
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo 'malicious code' > /workspace/.git/hooks/pre-commit",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # The command should fail because hooks directory is read-only
    assert result.returncode != 0
    combined = result.stdout + result.stderr
    assert (
        "read-only" in combined.lower()
        or "read only" in combined.lower()
        or "cannot create" in combined.lower()
        or "permission denied" in combined.lower()
    ), f"Expected read-only error, got: {combined}"


def test_git_hooks_readonly_touch_fails(coi_binary, workspace_dir, cleanup_containers):
    """Test that creating files in .git/hooks fails when protected."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Ensure hooks dir exists
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Try to touch a new file in hooks directory
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "touch",
            "/workspace/.git/hooks/test-hook",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should fail
    assert result.returncode != 0
    combined = result.stdout + result.stderr
    assert (
        "read-only" in combined.lower()
        or "read only" in combined.lower()
        or "cannot touch" in combined.lower()
        or "permission denied" in combined.lower()
    ), f"Expected read-only error, got: {combined}"


def test_git_hooks_readonly_mkdir_fails(coi_binary, workspace_dir, cleanup_containers):
    """Test that creating directories in .git/hooks fails when protected."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Ensure hooks dir exists
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Try to create a subdirectory in hooks
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "mkdir",
            "/workspace/.git/hooks/subdir",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should fail
    assert result.returncode != 0
    combined = result.stdout + result.stderr
    assert (
        "read-only" in combined.lower()
        or "read only" in combined.lower()
        or "cannot create" in combined.lower()
        or "permission denied" in combined.lower()
    ), f"Expected read-only error, got: {combined}"


def test_git_hooks_readonly_rm_fails(coi_binary, workspace_dir, cleanup_containers):
    """Test that removing files in .git/hooks fails when protected."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create a sample hook on the host
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)
    sample_hook = hooks_dir / "pre-commit.sample"
    sample_hook.write_text("#!/bin/sh\necho 'sample'\n")

    # Try to remove the sample hook from container
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "rm",
            "/workspace/.git/hooks/pre-commit.sample",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should fail
    assert result.returncode != 0
    combined = result.stdout + result.stderr
    assert (
        "read-only" in combined.lower()
        or "read only" in combined.lower()
        or "cannot remove" in combined.lower()
        or "permission denied" in combined.lower()
    ), f"Expected read-only error, got: {combined}"


def test_git_hooks_writable_with_flag(coi_binary, workspace_dir, cleanup_containers):
    """Test that --writable-git-hooks allows writing to .git/hooks."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Ensure hooks dir exists
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Run with --writable-git-hooks flag
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--writable-git-hooks",
            "--",
            "sh",
            "-c",
            "echo '#!/bin/sh' > /workspace/.git/hooks/pre-commit && chmod +x /workspace/.git/hooks/pre-commit",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should succeed
    assert result.returncode == 0, f"Command failed: {result.stderr}"

    # Verify the hook was created on the host
    hook_file = hooks_dir / "pre-commit"
    assert hook_file.exists(), "Hook file should have been created"
    assert "#!/bin/sh" in hook_file.read_text()


def test_git_hooks_writable_via_config(coi_binary, workspace_dir, cleanup_containers):
    """Test that writable_hooks=true in config allows writing to .git/hooks."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create config that enables writable hooks (disables protection)
    config_content = """
[git]
writable_hooks = true
"""
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    config_file = config_dir / "config.toml"
    config_file.write_text(config_content)

    # Ensure hooks dir exists
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Run command - protection should be disabled via config
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--",
            "sh",
            "-c",
            "echo '#!/bin/sh' > /workspace/.git/hooks/post-commit",
        ],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,  # Run from workspace to pick up .coi/config.toml
    )

    # Should succeed
    assert result.returncode == 0, f"Command failed: {result.stderr}"

    # Verify the hook was created
    hook_file = hooks_dir / "post-commit"
    assert hook_file.exists(), "Hook file should have been created"


def test_non_git_workspace_no_error(coi_binary, workspace_dir, cleanup_containers):
    """Test that non-git workspaces work without any hooks-related errors."""
    # workspace_dir is not a git repository (no .git directory)

    # Run a simple command
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
        timeout=120,
    )

    # Should succeed without any hooks-related errors
    assert result.returncode == 0
    assert "hello" in result.stdout

    # Should not mention git hooks in error output
    combined = result.stdout + result.stderr
    assert "git-hooks" not in combined.lower() or "protected" in combined.lower()


def test_git_hooks_existing_hook_readable(coi_binary, workspace_dir, cleanup_containers):
    """Test that existing hooks can be read from the container."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create a hook on the host
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)
    hook_content = "#!/bin/sh\necho 'Hello from pre-commit'\n"
    pre_commit = hooks_dir / "pre-commit"
    pre_commit.write_text(hook_content)
    pre_commit.chmod(0o755)

    # Read the hook from container
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "cat",
            "/workspace/.git/hooks/pre-commit",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should succeed and show hook content
    assert result.returncode == 0
    assert "Hello from pre-commit" in result.stdout


def test_git_hooks_existing_hook_executable(coi_binary, workspace_dir, cleanup_containers):
    """Test that existing hooks can be executed from the container."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create an executable hook on the host
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)
    hook_content = "#!/bin/sh\necho 'Hook executed successfully'\n"
    test_hook = hooks_dir / "test-hook"
    test_hook.write_text(hook_content)
    test_hook.chmod(0o755)

    # Execute the hook from container
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "/workspace/.git/hooks/test-hook",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should succeed
    assert result.returncode == 0
    assert "Hook executed successfully" in result.stdout


def test_git_hooks_rest_of_git_dir_writable(coi_binary, workspace_dir, cleanup_containers):
    """Test that other parts of .git directory are still writable."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Ensure hooks dir exists (for the protection to be applied)
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Create a file in .git (not in hooks)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo 'test' > /workspace/.git/test-file",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should succeed - only hooks is protected
    assert result.returncode == 0, f"Command failed: {result.stderr}"

    # Verify file was created
    test_file = Path(workspace_dir) / ".git" / "test-file"
    assert test_file.exists()
    assert test_file.read_text().strip() == "test"


def test_git_hooks_protection_log_message(coi_binary, workspace_dir, cleanup_containers):
    """Test that protection status is logged during setup."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Ensure hooks dir exists
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Run command and check stderr for protection message
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "echo",
            "test",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should mention hooks protection in setup output
    combined = result.stdout + result.stderr
    assert (
        "git/hooks" in combined.lower()
        or "git-hooks" in combined.lower()
        or "protected" in combined.lower()
    ), f"Expected hooks protection message, got: {combined}"


def test_git_hooks_empty_hooks_dir_created(coi_binary, workspace_dir, cleanup_containers):
    """Test that .git/hooks is created if missing before protection is applied."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Remove hooks directory if it exists (some git versions create it)
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    if hooks_dir.exists():
        import shutil

        shutil.rmtree(hooks_dir)

    # Verify hooks dir doesn't exist
    assert not hooks_dir.exists()

    # Run command - should create hooks dir and protect it
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "ls",
            "-la",
            "/workspace/.git/",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should succeed
    assert result.returncode == 0

    # Hooks directory should now exist on host (created by coi for protection)
    assert hooks_dir.exists(), "Hooks directory should have been created"


def test_git_hooks_protection_preserves_existing_hooks(
    coi_binary, workspace_dir, cleanup_containers
):
    """Test that protection preserves existing hooks content."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create multiple hooks on the host
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    hooks = {
        "pre-commit": "#!/bin/sh\necho 'pre-commit hook'\n",
        "post-commit": "#!/bin/sh\necho 'post-commit hook'\n",
        "pre-push": "#!/bin/sh\necho 'pre-push hook'\n",
    }

    for hook_name, content in hooks.items():
        hook_file = hooks_dir / hook_name
        hook_file.write_text(content)
        hook_file.chmod(0o755)

    # List hooks from container
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "ls",
            "/workspace/.git/hooks/",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # All hooks should be visible
    assert result.returncode == 0
    for hook_name in hooks:
        assert hook_name in result.stdout, f"Hook {hook_name} should be visible"


def test_git_hooks_modify_existing_hook_fails(coi_binary, workspace_dir, cleanup_containers):
    """Test that modifying an existing hook fails when protected."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create a hook on the host
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)
    pre_commit = hooks_dir / "pre-commit"
    pre_commit.write_text("#!/bin/sh\necho 'original'\n")
    pre_commit.chmod(0o755)

    # Try to modify the hook from container
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "sh",
            "-c",
            "echo 'malicious' >> /workspace/.git/hooks/pre-commit",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should fail
    assert result.returncode != 0

    # Original content should be preserved
    assert "original" in pre_commit.read_text()
    assert "malicious" not in pre_commit.read_text()


def test_git_hooks_chmod_fails(coi_binary, workspace_dir, cleanup_containers):
    """Test that chmod on hooks directory fails when protected."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create hooks dir
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Try to chmod the hooks directory from container
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "chmod",
            "777",
            "/workspace/.git/hooks",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should fail (can't change permissions on read-only mount)
    assert result.returncode != 0
    combined = result.stdout + result.stderr
    assert (
        "read-only" in combined.lower()
        or "read only" in combined.lower()
        or "operation not permitted" in combined.lower()
        or "permission denied" in combined.lower()
    ), f"Expected permission error, got: {combined}"


def test_git_hooks_symlink_attack_fails(coi_binary, workspace_dir, cleanup_containers):
    """Test that symlink attacks on hooks directory fail."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create hooks dir
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Try to create a symlink in hooks pointing elsewhere
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "ln",
            "-s",
            "/etc/passwd",
            "/workspace/.git/hooks/malicious-link",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should fail (read-only filesystem)
    assert result.returncode != 0


def test_git_hooks_hardlink_attack_fails(coi_binary, workspace_dir, cleanup_containers):
    """Test that hardlink creation in hooks directory fails."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create hooks dir and a file to link
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Create a file in workspace to try to hardlink
    test_file = Path(workspace_dir) / "test.txt"
    test_file.write_text("test content")

    # Try to create a hardlink in hooks
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "ln",
            "/workspace/test.txt",
            "/workspace/.git/hooks/hardlinked-file",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should fail (read-only filesystem)
    assert result.returncode != 0


def test_git_hooks_mv_into_fails(coi_binary, workspace_dir, cleanup_containers):
    """Test that moving files into hooks directory fails."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create hooks dir
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Create a file in workspace
    test_file = Path(workspace_dir) / "malicious-hook.sh"
    test_file.write_text("#!/bin/sh\necho 'malicious'\n")

    # Try to move it into hooks
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "mv",
            "/workspace/malicious-hook.sh",
            "/workspace/.git/hooks/pre-commit",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should fail
    assert result.returncode != 0

    # Original file should still exist
    assert test_file.exists()


def test_git_hooks_cp_into_fails(coi_binary, workspace_dir, cleanup_containers):
    """Test that copying files into hooks directory fails."""
    # Initialize git repo
    subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

    # Create hooks dir
    hooks_dir = Path(workspace_dir) / ".git" / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)

    # Create a file in workspace
    test_file = Path(workspace_dir) / "malicious-hook.sh"
    test_file.write_text("#!/bin/sh\necho 'malicious'\n")

    # Try to copy it into hooks
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "cp",
            "/workspace/malicious-hook.sh",
            "/workspace/.git/hooks/pre-commit",
        ],
        capture_output=True,
        text=True,
        timeout=120,
    )

    # Should fail
    assert result.returncode != 0

    # Hook should not exist
    pre_commit = hooks_dir / "pre-commit"
    assert not pre_commit.exists()
