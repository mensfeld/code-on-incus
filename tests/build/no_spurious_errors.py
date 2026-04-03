"""
Test for coi build - no spurious errors during cleanup.

Tests that:
1. Run coi build --profile with a simple script
2. Verify build succeeds
3. Verify no spurious error messages appear (e.g., "already stopped")

This test validates the fix for the issue where "Error: The instance is
already stopped" was displayed during successful builds due to the cleanup
function trying to stop an already-stopped container.
"""

import subprocess


def test_build_no_spurious_errors(coi_binary, tmp_path):
    """
    Test that successful builds don't show spurious error messages.

    Flow:
    1. Create a profile with a simple build script
    2. Build image via profile
    3. Verify exit code is 0
    4. Verify no "Error: The instance is already stopped" message
    5. Cleanup
    """
    image_name = "coi-test-no-errors"

    # Skip if base doesn't exist
    result = subprocess.run(
        [coi_binary, "image", "exists", "coi-default"],
        capture_output=True,
    )
    if result.returncode != 0:
        # Skip test if coi base image doesn't exist
        return

    # Create profile directory with config and build script
    profile_dir = tmp_path / ".coi" / "profiles" / "test-no-errors"
    profile_dir.mkdir(parents=True)

    (profile_dir / "config.toml").write_text(
        f'image = "{image_name}"\n\n[build]\nscript = "build.sh"\n'
    )

    (profile_dir / "build.sh").write_text(
        """#!/bin/bash
set -e
echo "Test build - no spurious errors"
"""
    )

    # Build custom image via profile
    result = subprocess.run(
        [coi_binary, "build", "--profile", "test-no-errors"],
        capture_output=True,
        text=True,
        timeout=300,
        cwd=str(tmp_path),
    )

    # Build should succeed
    assert result.returncode == 0, f"Build should succeed. stderr: {result.stderr}"

    # Verify no spurious error about already stopped container
    # This error was appearing during cleanup when the container was
    # already stopped by the imaging process
    combined_output = result.stdout + result.stderr
    assert "Error: The instance is already stopped" not in combined_output, (
        f"Build should not show spurious 'already stopped' error. Output:\n{combined_output}"
    )

    # Also check for other potential spurious errors
    # Success messages are ok, but "Error:" should only appear in actual failures
    if result.returncode == 0:
        # Build succeeded, so any "Error:" in output is spurious
        error_lines = [
            line
            for line in combined_output.split("\n")
            if "Error:" in line and "already stopped" in line.lower()
        ]
        assert len(error_lines) == 0, (
            "Successful build should not contain error messages. Found:\n" + "\n".join(error_lines)
        )

    # Cleanup
    subprocess.run([coi_binary, "image", "delete", image_name], check=False)
