"""
Integration tests for coi build --all.

Tests that:
1. coi build --all builds images for every profile that has a [container.build] section
2. Profiles without a build configuration are silently skipped
3. The second run with no --force skips already-built images
"""

import subprocess
from pathlib import Path


def test_build_all_builds_configured_profiles(coi_binary, tmp_path):
    """
    Test that 'coi build --all' builds images for all profiles with build configs.

    Flow:
    1. Create two profiles that each define a [container.build] section
    2. Create one profile with no build config (must be skipped)
    3. Run 'coi build --all'
    4. Verify both built images exist
    5. Run again without --force — should skip the already-built images
    6. Cleanup
    """
    image_alpha = "coi-test-all-alpha"
    image_beta = "coi-test-all-beta"

    # Skip the test when the base image is not available
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        return

    # Clean up any images left from a previous interrupted run
    for img in (image_alpha, image_beta):
        subprocess.run(
            [coi_binary, "image", "delete", img],
            check=False,
            capture_output=True,
        )

    # Profile alpha — has inline build commands
    profile_alpha = tmp_path / ".coi" / "profiles" / "alpha"
    profile_alpha.mkdir(parents=True)
    (profile_alpha / "config.toml").write_text(
        f'[container]\nimage = "{image_alpha}"\n\n'
        '[container.build]\nbase = "coi-default"\n'
        'commands = ["echo alpha > /tmp/marker.txt"]\n'
    )

    # Profile beta — also has inline build commands
    profile_beta = tmp_path / ".coi" / "profiles" / "beta"
    profile_beta.mkdir(parents=True)
    (profile_beta / "config.toml").write_text(
        f'[container]\nimage = "{image_beta}"\n\n'
        '[container.build]\nbase = "coi-default"\n'
        'commands = ["echo beta > /tmp/marker.txt"]\n'
    )

    # Profile gamma — no build config, must be silently skipped
    profile_gamma = tmp_path / ".coi" / "profiles" / "gamma"
    profile_gamma.mkdir(parents=True)
    (profile_gamma / "config.toml").write_text('[container]\nimage = "coi-default"\n')

    # === Run 1: build everything ===
    result = subprocess.run(
        [coi_binary, "build", "--all"],
        capture_output=True,
        text=True,
        timeout=600,
        cwd=str(tmp_path),
    )
    assert result.returncode == 0, f"coi build --all should succeed. stderr: {result.stderr}"

    # Both images must exist
    for img in (image_alpha, image_beta):
        check = subprocess.run(
            [coi_binary, "image", "exists", img],
            capture_output=True,
        )
        assert check.returncode == 0, f"Image '{img}' should exist after coi build --all"

    # === Run 2: images already built, expect "already exists" in output ===
    result = subprocess.run(
        [coi_binary, "build", "--all"],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=str(tmp_path),
    )
    assert result.returncode == 0, f"Second coi build --all should succeed. stderr: {result.stderr}"
    assert "already exists" in result.stderr.lower(), (
        "Second run should report that images already exist. stderr: " + result.stderr
    )

    # === Cleanup ===
    for img in (image_alpha, image_beta):
        subprocess.run(
            [coi_binary, "image", "delete", img],
            check=False,
            capture_output=True,
        )
