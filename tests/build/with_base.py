"""
Integration tests for custom image building.

Tests:
- coi build --profile with explicit base image
"""

import subprocess


def test_build_custom_with_base(coi_binary, tmp_path):
    """Test building a custom image with explicit base via profile."""
    image_name = "coi-test-custom-base"

    # Create profile directory with config and build script
    profile_dir = tmp_path / ".coi" / "profiles" / "test-base"
    profile_dir.mkdir(parents=True)

    (profile_dir / "config.toml").write_text(
        f'image = "{image_name}"\n\n[build]\nbase = "images:ubuntu/22.04"\nscript = "build.sh"\n'
    )

    (profile_dir / "build.sh").write_text("""#!/bin/bash
set -e
apt-get update
apt-get install -y jq
""")

    # Build custom image with ubuntu:22.04 as base (set in profile)
    result = subprocess.run(
        [coi_binary, "build", "--profile", "test-base"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=str(tmp_path),
    )
    assert result.returncode == 0, f"Build with base failed: {result.stderr}"

    # Verify image exists
    result = subprocess.run(
        [coi_binary, "image", "exists", image_name],
        capture_output=True,
    )
    assert result.returncode == 0, f"Custom image '{image_name}' should exist"

    # Cleanup
    subprocess.run([coi_binary, "image", "delete", image_name], check=False)
