"""
Test migration error for legacy root-level persistent in profile config.
"""

import subprocess
from pathlib import Path


def test_migration_error_profile_root_persistent(coi_binary, workspace_dir):
    """Root-level persistent in profile config produces a hard migration error."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "persprof"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text("persistent = true\n")

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, (
        f"Should fail with root-level persistent in profile. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "persistent" in combined.lower(), f"Error should mention persistent. Got:\n{combined}"
    assert "[container]" in combined, f"Error should suggest [container]. Got:\n{combined}"
