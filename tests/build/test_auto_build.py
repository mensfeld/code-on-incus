"""
Test auto-build on coi shell / coi run when image is missing.

Tests:
- coi run with missing image + [build] config → auto-builds, then runs
- coi shell with missing image + [build] config → auto-builds, then launches
- Missing image + no [build] config → existing error message
- Auto-build with missing script → clear error on run
- Auto-build with missing script → clear error on shell
- Auto-build with nonexistent base image → clear error on run
- Auto-build with nonexistent base image → clear error on shell
- --image flag overrides config defaults.image for auto-build check
"""

import subprocess
from pathlib import Path

import pytest


# Helper to skip if base coi image doesn't exist
def skip_without_coi(coi_binary):
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi"],
        capture_output=True,
    )
    if result.returncode != 0:
        pytest.skip("coi base image not found")


def test_auto_build_on_run(coi_binary, cleanup_containers, workspace_dir):
    """
    coi run with missing image + [build] config → auto-builds, then executes command.
    """
    skip_without_coi(coi_binary)
    image_name = "coi-test-auto-build-run"

    # Ensure image does NOT exist
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    # Create config with build section
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
commands = ["echo auto-built"]
"""
    )

    try:
        result = subprocess.run(
            [
                coi_binary,
                "run",
                "--workspace",
                workspace_dir,
                "--",
                "echo",
                "auto-build-works",
            ],
            capture_output=True,
            text=True,
            timeout=300,
            cwd=workspace_dir,
        )

        combined = result.stdout + result.stderr
        assert result.returncode == 0, f"Run with auto-build should succeed. Output:\n{combined}"
        assert "not found" in combined.lower() and "building" in combined.lower(), (
            f"Should mention auto-building. Got:\n{combined}"
        )
        assert "auto-build-works" in combined, (
            f"Command should have executed after auto-build. Got:\n{combined}"
        )
    finally:
        subprocess.run(
            [coi_binary, "image", "delete", image_name],
            check=False,
            capture_output=True,
        )


def test_auto_build_on_shell(coi_binary, cleanup_containers, workspace_dir):
    """
    coi shell --debug with missing image + [build] config → auto-builds, then launches.

    Uses --debug to get a bash shell instead of an AI tool, and runs a quick command.
    """
    skip_without_coi(coi_binary)
    image_name = "coi-test-auto-build-shell"

    # Ensure image does NOT exist
    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    # Create config with build section
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
commands = ["echo shell-auto-built"]
"""
    )

    try:
        from support.helpers import spawn_coi, wait_for_container_ready

        env = {"COI_USE_DUMMY": "1"}
        child = spawn_coi(
            coi_binary,
            [
                "shell",
                "--workspace",
                workspace_dir,
                "--debug",
                "--tmux=false",
            ],
            timeout=300,
            env=env,
            cwd=workspace_dir,
        )

        # Wait for auto-build to complete and container to be ready
        wait_for_container_ready(child, timeout=300)

        # If we got here, auto-build succeeded and shell launched
        child.sendline("echo shell-auto-build-ok")
        child.expect("shell-auto-build-ok", timeout=30)

        # Clean exit
        child.sendline("exit")
        child.expect(["\\$", "Cleaning up"], timeout=30)

    finally:
        subprocess.run(
            [coi_binary, "image", "delete", image_name],
            check=False,
            capture_output=True,
        )


def test_no_auto_build_without_config_on_run(coi_binary, workspace_dir):
    """
    coi run with missing image + no [build] config → standard 'not found' error.
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--image",
            "coi-nonexistent-image-12345",
            "--",
            "echo",
            "hello",
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Run should fail with missing image"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention image not found. Got:\n{combined}"
    )


def test_no_auto_build_without_config_on_shell(coi_binary, workspace_dir):
    """
    coi shell with missing image + no [build] config → standard 'not found' error.
    """
    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--image",
            "coi-nonexistent-image-67890",
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Shell should fail with missing image"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention image not found. Got:\n{combined}"
    )


def test_auto_build_missing_script_on_run(coi_binary, cleanup_containers, workspace_dir):
    """
    coi run with missing image + [build] pointing to nonexistent script → clear error.
    """
    image_name = "coi-test-auto-missing-script-run"

    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
script = "does-not-exist.sh"
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
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Run should fail when auto-build script is missing"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention script not found. Got:\n{combined}"
    )


def test_auto_build_missing_script_on_shell(coi_binary, workspace_dir):
    """
    coi shell with missing image + [build] pointing to nonexistent script → clear error.
    """
    image_name = "coi-test-auto-missing-script-shell"

    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
script = "missing-build-script.sh"
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Shell should fail when auto-build script is missing"
    combined = result.stdout + result.stderr
    assert "not found" in combined.lower(), (
        f"Error should mention script not found. Got:\n{combined}"
    )


def test_auto_build_nonexistent_base_on_run(coi_binary, cleanup_containers, workspace_dir):
    """
    coi run with missing image + [build] referencing nonexistent base → clear error.
    """
    image_name = "coi-test-auto-bad-base-run"

    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
base = "coi-totally-fake-image-xyz"
commands = ["echo hello"]
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
        timeout=120,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Run should fail when base image doesn't exist"
    combined = result.stdout + result.stderr
    assert (
        "fail" in combined.lower() or "error" in combined.lower() or "not found" in combined.lower()
    ), f"Error should indicate build failure. Got:\n{combined}"


def test_auto_build_nonexistent_base_on_shell(coi_binary, workspace_dir):
    """
    coi shell with missing image + [build] referencing nonexistent base → clear error.
    """
    image_name = "coi-test-auto-bad-base-shell"

    subprocess.run(
        [coi_binary, "image", "delete", image_name],
        check=False,
        capture_output=True,
    )

    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        f"""
[defaults]
image = "{image_name}"

[build]
base = "coi-fake-base-image-abc"
commands = ["echo hello"]
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "shell",
            "--workspace",
            workspace_dir,
            "--debug",
        ],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Shell should fail when base image doesn't exist"
    combined = result.stdout + result.stderr
    assert (
        "fail" in combined.lower() or "error" in combined.lower() or "not found" in combined.lower()
    ), f"Error should indicate build failure. Got:\n{combined}"


def test_image_flag_overrides_config_for_auto_build(coi_binary, cleanup_containers, workspace_dir):
    """
    --image flag should override defaults.image from config.

    If config says image = "coi-custom" with [build], but user passes --image coi,
    auto-build should NOT trigger (coi already exists).
    """
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        """
[defaults]
image = "coi-should-not-be-built"

[build]
commands = ["echo this-should-not-run"]
"""
    )

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--image",
            "coi",
            "--",
            "echo",
            "flag-override-works",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        cwd=workspace_dir,
    )

    combined = result.stdout + result.stderr
    # If coi image exists, this should succeed without building
    if result.returncode == 0:
        assert "building" not in combined.lower(), (
            f"Should NOT auto-build when --image flag overrides config. Got:\n{combined}"
        )
        assert "flag-override-works" in combined, (
            f"Command should execute with --image override. Got:\n{combined}"
        )
    else:
        # coi image might not exist in this environment — that's fine,
        # just make sure it didn't try to build "coi-should-not-be-built"
        assert "coi-should-not-be-built" not in combined, (
            f"Should use --image flag, not config defaults.image. Got:\n{combined}"
        )
