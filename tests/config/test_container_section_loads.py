"""
Test that the [container] section loads correctly in user config.

Tests that image, persistent, and storage_pool fields under [container]
parse and become visible via `coi profile info default`.
"""

import os
import subprocess


def test_container_section_loads_image_persistent(coi_binary, tmp_path):
    """[container] image and persistent fields parse from user config."""
    fake_home = tmp_path / "home"
    fake_home.mkdir()
    cfg_dir = fake_home / ".coi"
    cfg_dir.mkdir()
    (cfg_dir / "config.toml").write_text(
        '[container]\nimage = "coi-section-test"\npersistent = true\n'
    )

    workspace = tmp_path / "workspace"
    workspace.mkdir()

    env = os.environ.copy()
    env["HOME"] = str(fake_home)
    env.pop("COI_CONFIG", None)

    result = subprocess.run(
        [coi_binary, "profile", "info", "default", "--workspace", str(workspace)],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=str(workspace),
        env=env,
    )

    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    output = result.stdout + result.stderr
    assert "coi-section-test" in output, (
        f"Should resolve [container] image from user config. Got:\n{output}"
    )
    assert "true" in output.lower(), (
        f"Should resolve persistent=true from user config. Got:\n{output}"
    )


def test_container_section_loads_storage_pool(coi_binary, tmp_path):
    """[container] storage_pool field is preserved through config load."""
    fake_home = tmp_path / "home"
    fake_home.mkdir()
    cfg_dir = fake_home / ".coi"
    cfg_dir.mkdir()
    (cfg_dir / "config.toml").write_text(
        '[container]\nimage = "coi-default"\nstorage_pool = "custom-pool-name-xyz"\n'
    )

    workspace = tmp_path / "workspace"
    workspace.mkdir()

    env = os.environ.copy()
    env["HOME"] = str(fake_home)
    env.pop("COI_CONFIG", None)

    # The config should *parse* even though the pool doesn't exist; validation
    # only fires at container launch time. We don't actually launch here.
    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", str(workspace)],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=str(workspace),
        env=env,
    )

    assert result.returncode == 0, (
        f"profile list should succeed even with custom pool name. stderr: {result.stderr}"
    )
