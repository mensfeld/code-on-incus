"""
Test that `coi build` honours the profile's [container] storage_pool.

A profile with [container.build] and [container] storage_pool should
build the image into the requested pool.
"""

import subprocess

import pytest


def _create_temp_pool(name):
    result = subprocess.run(
        ["incus", "storage", "create", name, "dir"],
        capture_output=True,
        text=True,
        timeout=30,
    )
    return result.returncode == 0


def _delete_temp_pool(name):
    subprocess.run(
        ["incus", "storage", "delete", name],
        capture_output=True,
        timeout=30,
    )


def test_build_with_storage_pool(coi_binary, tmp_path):
    """coi build should succeed against a profile that selects a custom pool."""
    pool_name = "coi-test-buildpool"

    # Skip if base image isn't available
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-sandbox"],
        capture_output=True,
    )
    if result.returncode != 0:
        pytest.skip("coi-sandbox base image not present")

    if not _create_temp_pool(pool_name):
        pytest.skip("Cannot create temp storage pool")

    image_name = "coi-test-pool-build"

    try:
        # Cleanup any leftover image
        subprocess.run(
            [coi_binary, "image", "delete", image_name],
            check=False,
            capture_output=True,
        )

        # Profile referencing the temp pool with a build script
        profile_dir = tmp_path / ".coi" / "profiles" / "poolbuild"
        profile_dir.mkdir(parents=True)
        (profile_dir / "config.toml").write_text(
            f"[container]\n"
            f'image = "{image_name}"\n'
            f'storage_pool = "{pool_name}"\n\n'
            f"[container.build]\n"
            f'script = "build.sh"\n'
        )
        (profile_dir / "build.sh").write_text(
            "#!/bin/bash\nset -e\necho 'pool build ok' > /tmp/build_marker.txt\n"
        )

        result = subprocess.run(
            [coi_binary, "build", "--profile", "poolbuild"],
            capture_output=True,
            text=True,
            timeout=300,
            cwd=str(tmp_path),
        )
        assert result.returncode == 0, (
            f"Build with custom pool should succeed. stderr:\n{result.stderr}"
        )

        # Verify the image now exists
        check = subprocess.run(
            [coi_binary, "image", "exists", image_name],
            capture_output=True,
        )
        assert check.returncode == 0, "Built image should exist"
    finally:
        subprocess.run(
            [coi_binary, "image", "delete", image_name],
            check=False,
            capture_output=True,
        )
        _delete_temp_pool(pool_name)
