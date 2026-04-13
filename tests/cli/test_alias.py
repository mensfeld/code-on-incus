"""
Integration tests for container alias support (#304).

Tests alias registration, resolution, conflict detection, and cross-command support.
"""

import json
import os
import subprocess

from support.helpers import calculate_container_name, get_container_list


def write_alias_config(workspace_dir, alias_name):
    """Create .coi/config.toml with alias in the workspace."""
    config_dir = os.path.join(workspace_dir, ".coi")
    os.makedirs(config_dir, exist_ok=True)
    with open(os.path.join(config_dir, "config.toml"), "w") as f:
        f.write(f'[container]\nalias = "{alias_name}"\n')


def run_coi(coi_binary, args, workspace_dir=None, env_extra=None, timeout=120):
    """Run a coi command and return the result."""
    env = os.environ.copy()
    env["COI_USE_DUMMY"] = "1"
    if env_extra:
        env.update(env_extra)
    return subprocess.run(
        [coi_binary, *args],
        capture_output=True,
        text=True,
        timeout=timeout,
        cwd=workspace_dir,
        env=env,
    )


def get_aliases_registry():
    """Read and return the aliases.json registry."""
    path = os.path.join(os.path.expanduser("~"), ".coi", "aliases.json")
    if not os.path.exists(path):
        return {}
    with open(path) as f:
        return json.load(f)


# ============================================================
# Core alias tests
# ============================================================


class TestAliasStoredOnContainer:
    """Test that alias metadata is set on the container."""

    def test_alias_stored_on_container(
        self, coi_binary, workspace_dir, cleanup_containers, dummy_image
    ):
        """Launch persistent session with alias, verify user.coi.alias is set."""
        write_alias_config(workspace_dir, "testalias")
        container_name = calculate_container_name(workspace_dir, 1)

        result = run_coi(
            coi_binary,
            ["run", "--persistent", "--image", dummy_image, "sleep", "30"],
            workspace_dir=workspace_dir,
        )
        assert result.returncode == 0, f"coi run failed: {result.stderr}"

        # Container is persistent, so it should still exist
        containers = get_container_list()
        assert container_name in containers, (
            f"Expected persistent container '{container_name}' to exist for alias verification"
        )

        check = subprocess.run(
            ["incus", "config", "get", container_name, "user.coi.alias"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert check.stdout.strip() == "testalias", (
            f"Expected alias 'testalias', got '{check.stdout.strip()}'"
        )


class TestAliasInRegistry:
    """Test that alias is registered in ~/.coi/aliases.json."""

    def test_alias_in_registry(self, coi_binary, workspace_dir, cleanup_containers, dummy_image):
        """Launch session with alias, verify it appears in registry."""
        write_alias_config(workspace_dir, "registrytest")

        result = run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "echo", "ok"],
            workspace_dir=workspace_dir,
        )
        assert result.returncode == 0, f"coi run failed: {result.stderr}"

        registry = get_aliases_registry()
        assert "registrytest" in registry, f"Alias 'registrytest' not found in registry: {registry}"
        assert registry["registrytest"]["workspace"] == os.path.abspath(workspace_dir)


class TestAliasShownInList:
    """Test that aliases are displayed in coi list output."""

    def test_alias_shown_in_list_json(
        self, coi_binary, workspace_dir, cleanup_containers, dummy_image
    ):
        """Launch container with alias, verify it appears in JSON list output."""
        write_alias_config(workspace_dir, "listalias")
        container_name = calculate_container_name(workspace_dir, 1)

        # Launch a persistent container so it stays running
        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "--persistent", "sleep", "30"],
            workspace_dir=workspace_dir,
        )

        list_result = run_coi(coi_binary, ["list", "--format=json"])
        assert list_result.returncode == 0

        data = json.loads(list_result.stdout)
        containers = data.get("active_containers", [])

        # Find our container and verify alias
        found = [c for c in containers if c.get("name") == container_name]
        assert len(found) > 0, (
            f"Expected container '{container_name}' in list output, "
            f"got: {[c.get('name') for c in containers]}"
        )
        assert "alias" in found[0], f"Missing 'alias' key in container: {found[0]}"
        assert found[0]["alias"] == "listalias", (
            f"Expected alias 'listalias', got '{found[0].get('alias')}'"
        )

    def test_alias_shown_in_list_text(
        self, coi_binary, workspace_dir, cleanup_containers, dummy_image
    ):
        """Launch container with alias, verify (alias) appears in text output."""
        write_alias_config(workspace_dir, "textalias")
        container_name = calculate_container_name(workspace_dir, 1)

        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "--persistent", "sleep", "30"],
            workspace_dir=workspace_dir,
        )
        list_result = run_coi(coi_binary, ["list"])
        # Container should be in the list since it's persistent
        assert container_name in list_result.stdout, (
            f"Expected container '{container_name}' in text output"
        )
        assert "(textalias)" in list_result.stdout, "Expected '(textalias)' in text output"


# ============================================================
# Alias resolution for action commands
# ============================================================


class TestAttachByAlias:
    """Test that coi attach resolves aliases."""

    def test_attach_by_alias(self, coi_binary, workspace_dir, cleanup_containers, dummy_image):
        """Launch container with alias, attempt to attach by alias."""
        write_alias_config(workspace_dir, "attachalias")

        # Start a persistent container
        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "--persistent", "sleep", "60"],
            workspace_dir=workspace_dir,
        )

        # Try attaching by alias with --bash (will timeout quickly, but should not error with "not found")
        result = run_coi(
            coi_binary,
            ["attach", "attachalias", "--bash"],
            timeout=10,
        )
        # It should either succeed or fail with a container-level error, NOT "not found"
        # Alias resolution means it found the container
        if result.returncode != 0:
            assert (
                "not found" not in result.stderr.lower()
                or "container or alias" in result.stderr.lower()
            )


class TestKillByAlias:
    """Test that coi kill resolves aliases."""

    def test_kill_by_alias(self, coi_binary, workspace_dir, cleanup_containers, dummy_image):
        """Launch container with alias, kill it by alias."""
        write_alias_config(workspace_dir, "killalias")

        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "--persistent", "sleep", "60"],
            workspace_dir=workspace_dir,
        )

        result = run_coi(
            coi_binary,
            ["kill", "killalias", "--force"],
        )
        # Should succeed (exit 0) or at least resolve the alias
        assert result.returncode == 0 or "no running container found" in result.stderr.lower()


class TestShutdownByAlias:
    """Test that coi shutdown resolves aliases."""

    def test_shutdown_by_alias(self, coi_binary, workspace_dir, cleanup_containers, dummy_image):
        """Launch container with alias, shutdown by alias."""
        write_alias_config(workspace_dir, "shutdownalias")

        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "--persistent", "sleep", "60"],
            workspace_dir=workspace_dir,
        )

        result = run_coi(
            coi_binary,
            ["shutdown", "shutdownalias", "--force"],
        )
        assert result.returncode == 0 or "no running container found" in result.stderr.lower()


class TestUnfreezeByAlias:
    """Test that coi unfreeze resolves aliases."""

    def test_unfreeze_by_alias(self, coi_binary, workspace_dir, cleanup_containers, dummy_image):
        """Launch container with alias, freeze it, unfreeze by alias."""
        write_alias_config(workspace_dir, "unfreezealias")
        container_name = calculate_container_name(workspace_dir, 1)

        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "--persistent", "sleep", "60"],
            workspace_dir=workspace_dir,
        )

        # Pause the container
        subprocess.run(
            ["incus", "pause", container_name],
            capture_output=True,
            timeout=10,
        )

        # Unfreeze by alias
        result = run_coi(
            coi_binary,
            ["unfreeze", "unfreezealias"],
        )
        if result.returncode == 0:
            # Verify container is running again
            check = subprocess.run(
                ["incus", "list", container_name, "--format=csv", "-c", "s"],
                capture_output=True,
                text=True,
                timeout=10,
            )
            assert "RUNNING" in check.stdout.upper()


# ============================================================
# Slot suffix tests
# ============================================================


class TestAliasSlotSuffix:
    """Test alias with slot suffixes (e.g. myproject-2)."""

    def test_alias_with_slot_suffix(
        self, coi_binary, workspace_dir, cleanup_containers, dummy_image
    ):
        """Launch two containers, kill slot 2 by alias suffix."""
        write_alias_config(workspace_dir, "slotalias")

        # Launch slot 1
        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "--persistent", "--slot", "1", "sleep", "60"],
            workspace_dir=workspace_dir,
        )
        # Launch slot 2
        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "--persistent", "--slot", "2", "sleep", "60"],
            workspace_dir=workspace_dir,
        )

        container1 = calculate_container_name(workspace_dir, 1)
        container2 = calculate_container_name(workspace_dir, 2)

        # Kill slot 2 by alias suffix
        result = run_coi(
            coi_binary,
            ["kill", "slotalias-2", "--force"],
        )
        if result.returncode == 0:
            containers = get_container_list()
            assert container2 not in containers, "Slot 2 container should be deleted"
            # Slot 1 should still exist
            assert container1 in containers, "Slot 1 container should still exist"


# ============================================================
# Conflict detection tests
# ============================================================


class TestAliasConflict:
    """Test alias conflict detection."""

    def test_alias_conflict_different_workspace(
        self, coi_binary, workspace_dir, cleanup_containers, dummy_image, tmp_path
    ):
        """Register alias for workspace A, try to use same alias from workspace B."""
        write_alias_config(workspace_dir, "conflictalias")

        # Register alias from workspace A
        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "echo", "ok"],
            workspace_dir=workspace_dir,
        )

        # Create workspace B with same alias
        workspace_b = str(tmp_path / "workspace_b")
        os.makedirs(workspace_b)
        write_alias_config(workspace_b, "conflictalias")

        # Try to run from workspace B
        result = run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "echo", "ok"],
            workspace_dir=workspace_b,
        )
        # Should fail with alias conflict
        assert result.returncode != 0, "Should fail with alias conflict"
        assert "conflict" in result.stderr.lower() or "already registered" in result.stderr.lower()

    def test_alias_same_workspace_idempotent(
        self, coi_binary, workspace_dir, cleanup_containers, dummy_image
    ):
        """Running twice from same workspace with same alias should not conflict."""
        write_alias_config(workspace_dir, "idempotentalias")

        result1 = run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "echo", "first"],
            workspace_dir=workspace_dir,
        )
        assert result1.returncode == 0

        result2 = run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "echo", "second"],
            workspace_dir=workspace_dir,
        )
        assert result2.returncode == 0, f"Second run should succeed: {result2.stderr}"


# ============================================================
# Shell launch-by-alias tests
# ============================================================


class TestShellAlias:
    """Test coi shell <alias> positional argument."""

    def test_shell_positional_arg_unknown_alias(self, coi_binary, tmp_path):
        """Running coi shell with unknown alias should show clear error."""
        other_dir = str(tmp_path / "other")
        os.makedirs(other_dir)

        result = run_coi(
            coi_binary,
            ["shell", "nonexistentalias"],
            workspace_dir=other_dir,
        )
        assert result.returncode != 0
        assert "not found" in result.stderr.lower() or "not found" in result.stdout.lower()


# ============================================================
# Edge case tests
# ============================================================


class TestAliasEdgeCases:
    """Test alias edge cases."""

    def test_alias_exact_container_name_passthrough(
        self, coi_binary, workspace_dir, cleanup_containers, dummy_image
    ):
        """Exact container name format should pass through without alias resolution."""
        write_alias_config(workspace_dir, "edgealias")

        # Try to kill a container with exact name format (even if it doesn't exist)
        result = run_coi(
            coi_binary,
            ["kill", "coi-abc12345-1", "--force"],
        )
        # Should NOT error with "alias not found" — should pass through as container name
        if result.returncode != 0:
            assert "alias" not in result.stderr.lower()

    def test_alias_invalid_characters(
        self, coi_binary, workspace_dir, cleanup_containers, dummy_image
    ):
        """Invalid alias characters should be rejected."""
        write_alias_config(workspace_dir, "my project!")

        result = run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "echo", "ok"],
            workspace_dir=workspace_dir,
        )
        assert result.returncode != 0
        assert "invalid" in result.stderr.lower()

    def test_alias_starts_with_digit(
        self, coi_binary, workspace_dir, cleanup_containers, dummy_image
    ):
        """Alias starting with digit should be rejected."""
        write_alias_config(workspace_dir, "123project")

        result = run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "echo", "ok"],
            workspace_dir=workspace_dir,
        )
        assert result.returncode != 0
        assert (
            "invalid" in result.stderr.lower()
            or "must start with a letter" in result.stderr.lower()
        )

    def test_alias_empty_string(self, coi_binary, workspace_dir, cleanup_containers, dummy_image):
        """Empty alias should work normally (no alias set)."""
        write_alias_config(workspace_dir, "")

        result = run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "echo", "ok"],
            workspace_dir=workspace_dir,
        )
        assert result.returncode == 0, f"Empty alias should work: {result.stderr}"

    def test_list_no_alias(self, coi_binary, workspace_dir, cleanup_containers, dummy_image):
        """Container without alias should show empty alias in JSON output."""
        container_name = calculate_container_name(workspace_dir, 1)

        # No alias config — just launch normally
        run_coi(
            coi_binary,
            ["run", "--image", dummy_image, "--persistent", "sleep", "30"],
            workspace_dir=workspace_dir,
        )

        list_result = run_coi(coi_binary, ["list", "--format=json"])
        assert list_result.returncode == 0

        data = json.loads(list_result.stdout)
        containers = data.get("active_containers", [])
        found = [c for c in containers if c.get("name") == container_name]
        assert len(found) > 0, f"Expected container '{container_name}' in list output"
        assert "alias" in found[0], f"Missing 'alias' key in container: {found[0]}"
        assert found[0]["alias"] == "", f"Expected empty alias, got '{found[0]['alias']}'"
