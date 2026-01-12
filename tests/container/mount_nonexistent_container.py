"""
Test for coi container mount - fails for nonexistent container.

Tests that:
1. Try to mount into nonexistent container
2. Verify command fails
"""

import subprocess
import tempfile


def test_mount_nonexistent_container(coi_binary, cleanup_containers):
    """
    Test that mounting into nonexistent container fails.

    Flow:
    1. Create a temp directory
    2. Try to mount into nonexistent container
    3. Verify command fails with appropriate error
    """
    nonexistent_name = "nonexistent-container-12345"

    # === Phase 1: Create temp directory ===

    with tempfile.TemporaryDirectory() as tmpdir:
        # === Phase 2: Try to mount into nonexistent container ===
        # Syntax: coi container mount <name> <device-name> <source> <path>

        result = subprocess.run(
            [coi_binary, "container", "mount", nonexistent_name, "test-mount", tmpdir, "/mnt/test"],
            capture_output=True,
            text=True,
            timeout=30,
        )

        # === Phase 3: Verify failure ===

        assert result.returncode != 0, "Mounting into nonexistent container should fail"

        combined_output = result.stdout + result.stderr
        has_error = (
            "not found" in combined_output.lower()
            or "does not exist" in combined_output.lower()
            or "error" in combined_output.lower()
            or nonexistent_name in combined_output
        )

        assert has_error, f"Should indicate container not found. Got:\n{combined_output}"
