"""
Test migration error for legacy [defaults] persistent in user config.

Pre-0.8.0 layouts using [defaults] persistent must produce a hard error
with copy-pasteable migration guidance.
"""

import os
import subprocess


def test_migration_error_defaults_persistent(coi_binary, tmp_path):
    """[defaults] persistent in user config produces a hard migration error."""
    fake_home = tmp_path / "home"
    fake_home.mkdir()
    cfg_dir = fake_home / ".coi"
    cfg_dir.mkdir()
    (cfg_dir / "config.toml").write_text("[defaults]\npersistent = true\n")

    workspace = tmp_path / "workspace"
    workspace.mkdir()

    env = os.environ.copy()
    env["HOME"] = str(fake_home)
    env.pop("COI_CONFIG", None)

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", str(workspace)],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=str(workspace),
        env=env,
    )

    assert result.returncode != 0, (
        f"Should fail when [defaults] persistent is set. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "[defaults] persistent" in combined, (
        f"Error should mention [defaults] persistent. Got:\n{combined}"
    )
    assert "[container]" in combined, (
        f"Error should suggest the [container] section. Got:\n{combined}"
    )
