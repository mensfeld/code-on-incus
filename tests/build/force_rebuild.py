"""
Integration tests for custom image building.

Tests:
- coi build custom with script
- Custom image with base specified
- Custom image with privileged base
"""

import subprocess


def test_build_custom_force_rebuild(coi_binary, tmp_path):
    """Test force rebuilding an existing custom image."""
    image_name = "coi-test-custom-force"

    # Create build script
    build_script = tmp_path / "build_force.sh"
    build_script.write_text("""#!/bin/bash
set -e
echo "Build v1" > /tmp/version.txt
""")

    # Skip if base doesn't exist
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-sandbox"],
        capture_output=True,
    )
    if result.returncode != 0:
        return

    # Build first time
    result = subprocess.run(
        [coi_binary, "build", "custom", image_name, "--script", str(build_script)],
        capture_output=True,
        text=True,
        timeout=300,
    )
    assert result.returncode == 0, "First build should succeed"

    # Try to build again without --force (should skip)
    result = subprocess.run(
        [coi_binary, "build", "custom", image_name, "--script", str(build_script)],
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, "Build should succeed but skip"
    assert "already exists" in result.stderr.lower()

    # Update script
    build_script.write_text("""#!/bin/bash
set -e
echo "Build v2" > /tmp/version.txt
""")

    # Build with --force
    result = subprocess.run(
        [coi_binary, "build", "custom", image_name, "--script", str(build_script), "--force"],
        capture_output=True,
        text=True,
        timeout=300,
    )
    assert result.returncode == 0, "Force rebuild should succeed"

    # Cleanup
    subprocess.run([coi_binary, "image", "delete", image_name], check=False)
