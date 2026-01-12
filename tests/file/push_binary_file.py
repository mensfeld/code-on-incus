"""
Test for coi file push - push binary file.

Tests that:
1. Launch a container
2. Create a local binary file
3. Push file to container
4. Verify file exists with correct content (md5sum)
"""

import hashlib
import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_push_binary_file(coi_binary, cleanup_containers, workspace_dir):
    """
    Test pushing a binary file to a container.

    Flow:
    1. Launch a container
    2. Create a binary test file locally
    3. Push file to container
    4. Verify file content matches via md5sum
    5. Cleanup
    """
    container_name = calculate_container_name(workspace_dir, 1)

    # === Phase 1: Launch container ===

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"

    time.sleep(3)

    # === Phase 2: Create local binary file ===

    # Create binary content (bytes 0-255 repeated)
    binary_content = bytes(range(256)) * 10
    local_file = os.path.join(workspace_dir, "binary-test.bin")
    with open(local_file, "wb") as f:
        f.write(binary_content)

    # Calculate local md5sum
    local_md5 = hashlib.md5(binary_content).hexdigest()

    # === Phase 3: Push file to container ===

    result = subprocess.run(
        [coi_binary, "file", "push", local_file, f"{container_name}:/tmp/binary-test.bin"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"File push should succeed. stderr: {result.stderr}"

    # === Phase 4: Verify file content via md5sum ===

    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "md5sum", "/tmp/binary-test.bin"],
        capture_output=True,
        text=True,
        timeout=30,
    )

    assert result.returncode == 0, f"md5sum should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert local_md5 in combined_output, (
        f"MD5 should match. Expected {local_md5}, got:\n{combined_output}"
    )

    # === Phase 5: Cleanup ===

    subprocess.run(
        [coi_binary, "container", "delete", container_name, "--force"],
        capture_output=True,
        timeout=30,
    )
