"""
Integration tests for custom image building.

Tests:
- coi build custom with script
- Custom image with base specified
- Custom image with privileged base
"""

import json
import subprocess
import time


def test_build_custom_simple(coi_binary, tmp_path):
    """Test building a custom image with a simple script."""
    image_name = "coi-test-custom-simple"

    # Create build script
    build_script = tmp_path / "build.sh"
    build_script.write_text("""#!/bin/bash
set -e
apt-get update
apt-get install -y curl
echo "Custom build completed" > /tmp/build_marker.txt
""")

    # Build custom image (skip if coi-sandbox doesn't exist)
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-sandbox"],
        capture_output=True,
    )
    if result.returncode != 0:
        # Skip test if base image doesn't exist
        return

    # Cleanup any existing image from previous run
    subprocess.run([coi_binary, "image", "delete", image_name], check=False, capture_output=True)

    # Build custom image
    result = subprocess.run(
        [coi_binary, "build", "custom", image_name, "--script", str(build_script)],
        capture_output=True,
        text=True,
        timeout=300,  # 5 minutes
    )
    assert result.returncode == 0, f"Build failed: {result.stderr}"

    # Verify JSON output
    output = json.loads(result.stdout)
    assert "fingerprint" in output
    assert output["alias"] == image_name

    # Verify image exists
    result = subprocess.run(
        [coi_binary, "image", "exists", image_name],
        capture_output=True,
    )
    assert result.returncode == 0, "Custom image should exist"

    # Launch container from custom image to verify
    container_name = "coi-test-custom-verify"
    result = subprocess.run(
        [coi_binary, "container", "launch", image_name, container_name],
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, f"Launch from custom image failed: {result.stderr}"
    time.sleep(3)

    # Verify curl is installed (from our script)
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "which", "curl"],
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, "curl should be installed"

    # Cleanup
    subprocess.run([coi_binary, "container", "delete", container_name, "--force"], check=False)
    subprocess.run([coi_binary, "image", "delete", image_name], check=False)
