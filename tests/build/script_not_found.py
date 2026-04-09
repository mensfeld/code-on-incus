"""
Integration tests for custom image building.

Tests:
- coi build --profile with nonexistent script fails gracefully
"""

import subprocess


def test_build_custom_script_not_found(coi_binary, tmp_path):
    """Test that build fails with nonexistent script referenced in profile."""
    # Create profile directory with config referencing a nonexistent script
    profile_dir = tmp_path / ".coi" / "profiles" / "test-missing-script"
    profile_dir.mkdir(parents=True)

    (profile_dir / "config.toml").write_text(
        '[container]\nimage = "test-image"\n\n[container.build]\nscript = "/nonexistent/script.sh"\n'
    )

    result = subprocess.run(
        [coi_binary, "build", "--profile", "test-missing-script"],
        capture_output=True,
        text=True,
        cwd=str(tmp_path),
    )
    assert result.returncode != 0, "Build should fail with nonexistent script"
    assert "not found" in result.stderr.lower() or "does not exist" in result.stderr.lower()
