"""Test extended security protection feature.

This feature extends the git hooks protection to support configurable protected paths.
Default protected paths include:
- .git/hooks (git hooks execute on git operations)
- .git/config (can set core.hooksPath to bypass hooks protection)
- .husky (husky git hooks manager)
- .vscode (VS Code tasks.json can auto-execute, settings.json can inject shell args)

Configuration options:
- [security] protected_paths - Replace default protected paths list
- [security] additional_protected_paths - Add paths without replacing defaults
- [security] disable_protection - Disable all path protection
"""

import subprocess
from pathlib import Path


class TestGitConfigProtection:
    """Tests for .git/config protection (prevents core.hooksPath bypass)."""

    def test_git_config_readonly_by_default(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that .git/config is mounted read-only by default."""
        # Initialize a git repository
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

        # Ensure .git/config exists
        git_config = Path(workspace_dir) / ".git" / "config"
        assert git_config.exists(), ".git/config should exist after git init"

        # Try to modify .git/config from container
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "sh",
                "-c",
                "echo '[core]' >> /workspace/.git/config",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        # Should fail because .git/config is read-only
        assert result.returncode != 0
        combined = result.stdout + result.stderr
        assert (
            "read-only" in combined.lower()
            or "read only" in combined.lower()
            or "permission denied" in combined.lower()
        ), f"Expected read-only error, got: {combined}"

    def test_git_config_core_hookspath_attack_blocked(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """Test that setting core.hooksPath via git config fails."""
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

        # Try to set core.hooksPath to a malicious location
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "git",
                "-C",
                "/workspace",
                "config",
                "core.hooksPath",
                "/tmp/malicious-hooks",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        # Should fail because .git/config is read-only
        assert result.returncode != 0
        combined = result.stdout + result.stderr
        assert (
            "read-only" in combined.lower()
            or "read only" in combined.lower()
            or "could not lock" in combined.lower()
            or "unable to create" in combined.lower()
        ), f"Expected config lock error, got: {combined}"

    def test_git_config_readable(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that .git/config can be read from container."""
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

        # Read .git/config from container
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "cat",
                "/workspace/.git/config",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        # Should succeed and show config content
        assert result.returncode == 0
        # Git config should have at least [core] section
        assert "[core]" in result.stdout or "repositoryformatversion" in result.stdout


class TestHuskyProtection:
    """Tests for .husky directory protection (husky git hooks manager)."""

    def test_husky_readonly_by_default(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that .husky directory is mounted read-only by default."""
        # Create .husky directory (simulating a project using husky)
        husky_dir = Path(workspace_dir) / ".husky"
        husky_dir.mkdir(parents=True, exist_ok=True)

        # Create a pre-commit hook
        pre_commit = husky_dir / "pre-commit"
        pre_commit.write_text("#!/bin/sh\necho 'Original hook'\n")

        # Try to write to .husky from container
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "sh",
                "-c",
                "echo 'malicious' > /workspace/.husky/pre-commit",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        # Should fail because .husky is read-only
        assert result.returncode != 0
        combined = result.stdout + result.stderr
        assert (
            "read-only" in combined.lower()
            or "read only" in combined.lower()
            or "permission denied" in combined.lower()
        ), f"Expected read-only error, got: {combined}"

        # Original content should be preserved
        assert "Original hook" in pre_commit.read_text()

    def test_husky_touch_fails(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that creating files in .husky fails when protected."""
        husky_dir = Path(workspace_dir) / ".husky"
        husky_dir.mkdir(parents=True, exist_ok=True)

        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "touch",
                "/workspace/.husky/new-hook",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode != 0
        combined = result.stdout + result.stderr
        assert (
            "read-only" in combined.lower()
            or "read only" in combined.lower()
            or "permission denied" in combined.lower()
        ), f"Expected read-only error, got: {combined}"

    def test_husky_readable(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that .husky files can be read from container."""
        husky_dir = Path(workspace_dir) / ".husky"
        husky_dir.mkdir(parents=True, exist_ok=True)

        pre_commit = husky_dir / "pre-commit"
        pre_commit.write_text("#!/bin/sh\necho 'Husky pre-commit'\n")

        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "cat",
                "/workspace/.husky/pre-commit",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0
        assert "Husky pre-commit" in result.stdout

    def test_husky_nonexistent_no_error(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that missing .husky directory doesn't cause errors."""
        # workspace_dir has no .husky directory

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

        # Should succeed without any husky-related errors
        assert result.returncode == 0
        assert "hello" in result.stdout


class TestVscodeProtection:
    """Tests for .vscode directory protection (prevents tasks.json auto-execute attacks)."""

    def test_vscode_readonly_by_default(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that .vscode directory is mounted read-only by default."""
        vscode_dir = Path(workspace_dir) / ".vscode"
        vscode_dir.mkdir(parents=True, exist_ok=True)

        # Create a tasks.json
        tasks_json = vscode_dir / "tasks.json"
        tasks_json.write_text('{"version": "2.0.0", "tasks": []}\n')

        # Try to write to .vscode from container
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "sh",
                "-c",
                "echo 'malicious' > /workspace/.vscode/tasks.json",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode != 0
        combined = result.stdout + result.stderr
        assert (
            "read-only" in combined.lower()
            or "read only" in combined.lower()
            or "permission denied" in combined.lower()
        ), f"Expected read-only error, got: {combined}"

    def test_vscode_settings_json_protected(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that .vscode/settings.json cannot be modified."""
        vscode_dir = Path(workspace_dir) / ".vscode"
        vscode_dir.mkdir(parents=True, exist_ok=True)

        settings_json = vscode_dir / "settings.json"
        settings_json.write_text('{"editor.fontSize": 14}\n')

        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "sh",
                "-c",
                'echo \'{"terminal.integrated.shellArgs.linux": ["-c", "malicious"]}\' > /workspace/.vscode/settings.json',
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode != 0
        # Original content should be preserved
        assert "editor.fontSize" in settings_json.read_text()

    def test_vscode_readable(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that .vscode files can be read from container."""
        vscode_dir = Path(workspace_dir) / ".vscode"
        vscode_dir.mkdir(parents=True, exist_ok=True)

        settings_json = vscode_dir / "settings.json"
        settings_json.write_text('{"editor.fontSize": 16}\n')

        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "cat",
                "/workspace/.vscode/settings.json",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0
        assert "editor.fontSize" in result.stdout


class TestSecurityConfigAdditionalPaths:
    """Tests for additional_protected_paths configuration."""

    def test_additional_paths_are_protected(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that additional_protected_paths adds to defaults."""
        # Create config that adds .idea to protected paths
        config_content = """
[security]
additional_protected_paths = [".idea"]
"""
        config_dir = Path(workspace_dir) / ".coi"
        config_dir.mkdir(exist_ok=True)
        config_file = config_dir / "config.toml"
        config_file.write_text(config_content)

        # Create .idea directory (simulating IntelliJ project)
        idea_dir = Path(workspace_dir) / ".idea"
        idea_dir.mkdir(parents=True, exist_ok=True)
        workspace_xml = idea_dir / "workspace.xml"
        workspace_xml.write_text("<project></project>")

        # Run command from workspace directory to pick up config
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--",
                "sh",
                "-c",
                "echo 'malicious' > /workspace/.idea/workspace.xml",
            ],
            capture_output=True,
            text=True,
            timeout=120,
            cwd=workspace_dir,
        )

        # Should fail because .idea is now protected
        assert result.returncode != 0
        combined = result.stdout + result.stderr
        assert (
            "read-only" in combined.lower()
            or "read only" in combined.lower()
            or "permission denied" in combined.lower()
        ), f"Expected read-only error, got: {combined}"

    def test_additional_paths_preserves_defaults(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """Test that additional_protected_paths doesn't replace defaults."""
        # Create config that adds .idea
        config_content = """
[security]
additional_protected_paths = [".idea"]
"""
        config_dir = Path(workspace_dir) / ".coi"
        config_dir.mkdir(exist_ok=True)
        config_file = config_dir / "config.toml"
        config_file.write_text(config_content)

        # Initialize git repo (for .git/hooks default protection)
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

        # Ensure hooks dir exists
        hooks_dir = Path(workspace_dir) / ".git" / "hooks"
        hooks_dir.mkdir(parents=True, exist_ok=True)

        # Run command - default .git/hooks should still be protected
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--",
                "touch",
                "/workspace/.git/hooks/new-hook",
            ],
            capture_output=True,
            text=True,
            timeout=120,
            cwd=workspace_dir,
        )

        # Should fail because .git/hooks is still protected (default)
        assert result.returncode != 0


class TestSecurityConfigCustomPaths:
    """Tests for protected_paths configuration (replaces defaults)."""

    def test_custom_paths_replace_defaults(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that protected_paths replaces the default list."""
        # Create config with only .git/hooks (not other defaults)
        config_content = """
[security]
protected_paths = [".git/hooks"]
"""
        config_dir = Path(workspace_dir) / ".coi"
        config_dir.mkdir(exist_ok=True)
        config_file = config_dir / "config.toml"
        config_file.write_text(config_content)

        # Initialize git repo
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

        # Create .vscode (would be protected by default, but not with our custom list)
        vscode_dir = Path(workspace_dir) / ".vscode"
        vscode_dir.mkdir(parents=True, exist_ok=True)

        # Try to write to .vscode - should succeed since it's not in custom list
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--",
                "touch",
                "/workspace/.vscode/test.json",
            ],
            capture_output=True,
            text=True,
            timeout=120,
            cwd=workspace_dir,
        )

        # Should succeed because .vscode is not in our custom protected_paths
        assert result.returncode == 0, f"Expected success, got: {result.stderr}"

        # Verify file was created
        test_file = vscode_dir / "test.json"
        assert test_file.exists()


class TestSecurityConfigDisableProtection:
    """Tests for disable_protection configuration."""

    def test_disable_protection_allows_all(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that disable_protection=true allows writing to all paths."""
        # Create config that disables protection
        config_content = """
[security]
disable_protection = true
"""
        config_dir = Path(workspace_dir) / ".coi"
        config_dir.mkdir(exist_ok=True)
        config_file = config_dir / "config.toml"
        config_file.write_text(config_content)

        # Initialize git repo
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

        # Ensure hooks dir exists
        hooks_dir = Path(workspace_dir) / ".git" / "hooks"
        hooks_dir.mkdir(parents=True, exist_ok=True)

        # Run command - should be able to write to hooks
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--",
                "sh",
                "-c",
                "echo '#!/bin/sh' > /workspace/.git/hooks/pre-commit",
            ],
            capture_output=True,
            text=True,
            timeout=120,
            cwd=workspace_dir,
        )

        # Should succeed because protection is disabled
        assert result.returncode == 0, f"Command failed: {result.stderr}"

        # Verify hook was created
        hook_file = hooks_dir / "pre-commit"
        assert hook_file.exists()


class TestSecurityLogging:
    """Tests for security protection logging."""

    def test_protection_logged_for_multiple_paths(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """Test that protection status is logged for multiple paths."""
        # Initialize git repo
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

        # Create multiple protected paths
        hooks_dir = Path(workspace_dir) / ".git" / "hooks"
        hooks_dir.mkdir(parents=True, exist_ok=True)

        vscode_dir = Path(workspace_dir) / ".vscode"
        vscode_dir.mkdir(parents=True, exist_ok=True)

        # Run command and check for protection message
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

        assert result.returncode == 0
        combined = result.stdout + result.stderr

        # Should mention protected paths
        assert "protected" in combined.lower(), f"Expected protection message, got: {combined}"


class TestSymlinkSecurity:
    """Tests for symlink security (preventing mount of arbitrary host paths)."""

    def test_symlinked_protected_path_rejected(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that symlinked protected paths are rejected."""
        # Create a target directory
        target_dir = Path(workspace_dir) / "target"
        target_dir.mkdir(parents=True, exist_ok=True)

        # Create .vscode as a symlink to target
        vscode_link = Path(workspace_dir) / ".vscode"
        vscode_link.symlink_to(target_dir)

        # Run command - should not error but symlink should not be mounted
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

        # Should succeed (symlink just skipped, not an error)
        assert result.returncode == 0

    def test_symlinked_git_dir_handled(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that .git as symlink (worktree) is handled gracefully."""
        # Create a target directory
        target_dir = Path(workspace_dir) / "git-target"
        target_dir.mkdir(parents=True, exist_ok=True)

        # Create .git as a symlink (simulating git worktree)
        git_link = Path(workspace_dir) / ".git"
        git_link.symlink_to(target_dir)

        # Run command - should not error
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

        # Should succeed - symlinked .git is skipped gracefully
        assert result.returncode == 0

    def test_git_file_handled(self, coi_binary, workspace_dir, cleanup_containers):
        """Test that .git as file (submodule/worktree) is handled gracefully."""
        # Create .git as a file (simulating submodule or worktree)
        git_file = Path(workspace_dir) / ".git"
        git_file.write_text("gitdir: /some/other/path\n")

        # Run command - should not error
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

        # Should succeed - .git as file is skipped gracefully
        assert result.returncode == 0


class TestWritableGitHooksFlagBackwardsCompat:
    """Tests for --writable-git-hooks flag backwards compatibility."""

    def test_writable_git_hooks_disables_all_protection(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """Test that --writable-git-hooks disables all path protection."""
        # Initialize git repo
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

        # Create protected paths
        hooks_dir = Path(workspace_dir) / ".git" / "hooks"
        hooks_dir.mkdir(parents=True, exist_ok=True)

        vscode_dir = Path(workspace_dir) / ".vscode"
        vscode_dir.mkdir(parents=True, exist_ok=True)

        # Run with --writable-git-hooks - should disable all protection
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
                "touch /workspace/.git/hooks/test && touch /workspace/.vscode/test",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        # Should succeed
        assert result.returncode == 0, f"Command failed: {result.stderr}"

        # Both files should be created
        assert (hooks_dir / "test").exists()
        assert (vscode_dir / "test").exists()

    def test_config_writable_hooks_disables_all_protection(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """Test that [git] writable_hooks=true disables all protection."""
        # Create config with writable_hooks
        config_content = """
[git]
writable_hooks = true
"""
        config_dir = Path(workspace_dir) / ".coi"
        config_dir.mkdir(exist_ok=True)
        config_file = config_dir / "config.toml"
        config_file.write_text(config_content)

        # Initialize git repo
        subprocess.run(["git", "init"], cwd=workspace_dir, check=True, capture_output=True)

        # Create protected paths
        hooks_dir = Path(workspace_dir) / ".git" / "hooks"
        hooks_dir.mkdir(parents=True, exist_ok=True)

        husky_dir = Path(workspace_dir) / ".husky"
        husky_dir.mkdir(parents=True, exist_ok=True)

        # Run from workspace to pick up config
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--",
                "sh",
                "-c",
                "touch /workspace/.git/hooks/test && touch /workspace/.husky/test",
            ],
            capture_output=True,
            text=True,
            timeout=120,
            cwd=workspace_dir,
        )

        # Should succeed
        assert result.returncode == 0, f"Command failed: {result.stderr}"

        # Both files should be created
        assert (hooks_dir / "test").exists()
        assert (husky_dir / "test").exists()
