"""
Test migration error for legacy root-level [build] in profile config.
"""

import subprocess
from pathlib import Path


def test_migration_error_profile_root_build(coi_binary, workspace_dir):
    """Root-level [build] in profile config produces a hard migration error."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "buildprof"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('[build]\nbase = "coi-default"\nscript = "build.sh"\n')

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, (
        f"Should fail with root-level [build] in profile. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "[build]" in combined, f"Error should mention [build]. Got:\n{combined}"
    assert "[container.build]" in combined, (
        f"Error should suggest [container.build]. Got:\n{combined}"
    )
