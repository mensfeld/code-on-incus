"""
Integration tests for the --compression flag in build commands.

Currently covered:
- coi build --compression none
- coi build --profile <name> --compression none
"""

import subprocess
import time


def test_build_with_compression_none(coi_binary):
    """Test building the coi image with --compression none flag."""
    # Build coi image with --compression none --force
    result = subprocess.run(
        [coi_binary, "build", "--compression", "none", "--force"],
        capture_output=True,
        text=True,
        timeout=600,
    )
    assert result.returncode == 0, f"Build failed: {result.stderr}"
    assert (
        "built successfully" in result.stdout.lower()
        or "built successfully" in result.stderr.lower()
    )

    # Verify image exists
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    assert result.returncode == 0, "coi image should exist after build"


def test_build_custom_with_compression_none(coi_binary, tmp_path):
    """Test building a custom image with --compression none flag via profile."""
    image_name = "coi-test-compression"

    # Build custom image (skip if coi doesn't exist)
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        # Skip test if base image doesn't exist
        return

    # Cleanup any existing image from previous run
    subprocess.run([coi_binary, "image", "delete", image_name], check=False, capture_output=True)

    # Create profile directory with config and build script
    profile_dir = tmp_path / ".coi" / "profiles" / "test-compression"
    profile_dir.mkdir(parents=True)

    (profile_dir / "config.toml").write_text(
        f'[container]\nimage = "{image_name}"\n\n[container.build]\nscript = "build.sh"\n'
    )

    (profile_dir / "build.sh").write_text("""#!/bin/bash
set -e
apt-get update
apt-get install -y curl
echo "Custom build completed" > /tmp/build_marker.txt
""")

    # Build custom image with --compression none
    result = subprocess.run(
        [coi_binary, "build", "--profile", "test-compression", "--compression", "none"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=str(tmp_path),
    )
    assert result.returncode == 0, f"Build failed: {result.stderr}"

    # Verify image exists
    result = subprocess.run(
        [coi_binary, "image", "exists", image_name],
        capture_output=True,
    )
    assert result.returncode == 0, "Custom image should exist"

    # Launch container from custom image to verify
    container_name = "coi-test-compression-verify"
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
