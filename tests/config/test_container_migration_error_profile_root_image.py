"""
Test migration error for legacy root-level image in profile config.

Pre-0.8.0 profile layouts with `image = "..."` at the file root must
produce a hard error with copy-pasteable migration guidance.
"""

import subprocess
from pathlib import Path


def test_migration_error_profile_root_image(coi_binary, workspace_dir):
    """Root-level image in profile config produces a hard migration error."""
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / "legacyprof"
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text('image = "coi-legacy-profile"\n')

    result = subprocess.run(
        [coi_binary, "profile", "list", "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, (
        f"Should fail with root-level image in profile. stdout: {result.stdout}"
    )
    combined = result.stdout + result.stderr
    assert "Root-level image" in combined or "root-level image" in combined.lower(), (
        f"Error should mention root-level image. Got:\n{combined}"
    )
    assert "[container]" in combined, f"Error should suggest [container]. Got:\n{combined}"
    assert str(profile_dir / "config.toml") in combined, (
        f"Error should reference the offending file path. Got:\n{combined}"
    )
