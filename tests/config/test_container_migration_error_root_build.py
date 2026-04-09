"""
Test migration error for legacy top-level [build] in user config.

Pre-0.8.0 layouts using a top-level [build] section in global config must
produce a hard error pointing to the new [container.build] location.
"""

import os
import subprocess


def test_migration_error_root_build(coi_binary, tmp_path):
    """Top-level [build] in user config produces a hard migration error."""
    fake_home = tmp_path / "home"
    fake_home.mkdir()
    cfg_dir = fake_home / ".coi"
    cfg_dir.mkdir()
    (cfg_dir / "config.toml").write_text('[build]\nbase = "coi-default"\nscript = "build.sh"\n')

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
        f"Should fail when top-level [build] is set. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "[build]" in combined, f"Error should mention [build]. Got:\n{combined}"
    assert "[container.build]" in combined, (
        f"Error should suggest [container.build]. Got:\n{combined}"
    )
