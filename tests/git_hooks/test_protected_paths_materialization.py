"""Regression tests for FLAWS.md Finding 2 — silent skip of non-existent protected paths.

Prior to P1-1, any entry in `security.protected_paths` that did not already
exist on the host was silently skipped (only `.git/hooks` was auto-created).
A process inside the container could then `mkdir /workspace/.vscode && echo
pwn > /workspace/.vscode/tasks.json` and the write would persist to the host
workspace, enabling a tasks.json auto-execute attack the next time the host
user opened the repo.

P1-1 materializes every protected entry before mounting it read-only. These
end-to-end tests spawn a real container in a fresh workspace (no pre-existing
`.vscode`, `.husky`, `.idea`, or `.git/config`) and assert that writes to each
protected path fail from inside the container and do not persist to the host.
"""

import subprocess
from pathlib import Path


class TestVscodeMaterializedWhenMissing:
    """Finding 2 regression: .vscode must be protected even if it didn't exist."""

    def test_vscode_create_blocked_in_empty_workspace(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """Writing /workspace/.vscode/tasks.json from inside the container must fail."""
        # Sanity: workspace starts clean.
        assert not (Path(workspace_dir) / ".vscode").exists()

        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "sh",
                "-c",
                "echo pwn > /workspace/.vscode/tasks.json",
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

        # Nothing must have persisted to the host.
        tasks_json = Path(workspace_dir) / ".vscode" / "tasks.json"
        if tasks_json.exists():
            assert tasks_json.read_text() == "", (
                f"tasks.json should not contain attacker data, got: {tasks_json.read_text()!r}"
            )

    def test_vscode_dir_materialized_and_empty(self, coi_binary, workspace_dir, cleanup_containers):
        """After `coi run`, host .vscode/ must exist as an empty directory."""
        assert not (Path(workspace_dir) / ".vscode").exists()

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
        assert result.returncode == 0, f"coi run failed: {result.stderr}"

        vscode_dir = Path(workspace_dir) / ".vscode"
        assert vscode_dir.exists(), ".vscode must be materialized on the host"
        assert vscode_dir.is_dir(), ".vscode must be a directory"
        assert list(vscode_dir.iterdir()) == [], ".vscode must be empty"


class TestHuskyMaterializedWhenMissing:
    """Finding 2 regression: .husky must be protected even if it didn't exist."""

    def test_husky_create_blocked_in_empty_workspace(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """Writing /workspace/.husky/pre-commit from inside the container must fail."""
        assert not (Path(workspace_dir) / ".husky").exists()

        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "sh",
                "-c",
                "echo pwn > /workspace/.husky/pre-commit",
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

        pre_commit = Path(workspace_dir) / ".husky" / "pre-commit"
        if pre_commit.exists():
            assert pre_commit.read_text() == "", (
                f"pre-commit should not contain attacker data, got: {pre_commit.read_text()!r}"
            )

    def test_husky_dir_materialized_and_empty(self, coi_binary, workspace_dir, cleanup_containers):
        """After `coi run`, host .husky/ must exist as an empty directory."""
        assert not (Path(workspace_dir) / ".husky").exists()

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
        assert result.returncode == 0, f"coi run failed: {result.stderr}"

        husky_dir = Path(workspace_dir) / ".husky"
        assert husky_dir.exists(), ".husky must be materialized on the host"
        assert husky_dir.is_dir(), ".husky must be a directory"
        assert list(husky_dir.iterdir()) == [], ".husky must be empty"


class TestGitConfigMaterialization:
    """File-type protected path: .git/config placeholder when parent .git/ exists."""

    def test_git_config_placeholder_in_git_dir_without_config(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """With .git/ present but no .git/config, writes to .git/config must fail."""
        git_dir = Path(workspace_dir) / ".git"
        git_dir.mkdir()
        # No config file — exercise the file-type placeholder path.
        assert not (git_dir / "config").exists()

        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "sh",
                "-c",
                "echo attack > /workspace/.git/config",
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

        # Placeholder must exist on the host and be empty.
        config_path = git_dir / "config"
        assert config_path.exists(), ".git/config placeholder must be materialized"
        assert config_path.is_file(), ".git/config must be a regular file"
        assert config_path.read_text() == "", (
            f".git/config should be empty placeholder, got: {config_path.read_text()!r}"
        )

    def test_git_config_skipped_in_non_git_workspace(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """With no .git/ at all, coi must still run cleanly and not synthesize .git/."""
        assert not (Path(workspace_dir) / ".git").exists()

        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "echo",
                "ok",
            ],
            capture_output=True,
            text=True,
            timeout=120,
        )

        assert result.returncode == 0, f"coi run failed: {result.stderr}"
        assert "ok" in result.stdout

        # .git/ must NOT have been synthesized.
        assert not (Path(workspace_dir) / ".git").exists(), (
            ".git/ must not be created in a non-git workspace"
        )


class TestAdditionalProtectedPathsMaterialized:
    """Materialization must cover user-declared additional_protected_paths too."""

    def test_idea_materialized_via_additional_paths(
        self, coi_binary, workspace_dir, cleanup_containers
    ):
        """Adding .idea via additional_protected_paths must protect it even when missing."""
        config_content = """
[security]
additional_protected_paths = [".idea"]
"""
        config_dir = Path(workspace_dir) / ".coi"
        config_dir.mkdir()
        (config_dir / "config.toml").write_text(config_content)

        assert not (Path(workspace_dir) / ".idea").exists()

        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--",
                "sh",
                "-c",
                "echo pwn > /workspace/.idea/workspace.xml",
            ],
            capture_output=True,
            text=True,
            timeout=120,
            cwd=workspace_dir,
        )

        assert result.returncode != 0
        combined = result.stdout + result.stderr
        assert (
            "read-only" in combined.lower()
            or "read only" in combined.lower()
            or "permission denied" in combined.lower()
        ), f"Expected read-only error, got: {combined}"

        idea_dir = Path(workspace_dir) / ".idea"
        assert idea_dir.exists(), ".idea must be materialized on the host"
        assert idea_dir.is_dir()
        assert list(idea_dir.iterdir()) == [], ".idea must be empty"
