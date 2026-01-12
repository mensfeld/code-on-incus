"""
Test for coi run - workspace is mounted at /workspace.

Tests that:
1. Create a file in local workspace
2. Run command that reads the file
3. Verify file is accessible in container
"""

import os
import subprocess


def test_run_workspace_mounted(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that workspace directory is mounted at /workspace.

    Flow:
    1. Create a test file in workspace
    2. Run coi run to cat the file
    3. Verify file content is accessible
    """
    # Create a test file in workspace
    test_content = "workspace-mount-test-content-abc123"
    test_file = os.path.join(workspace_dir, "mount-test.txt")
    with open(test_file, "w") as f:
        f.write(test_content)

    # Run command to read the file
    result = subprocess.run(
        [coi_binary, "run", "--workspace", workspace_dir, "cat", "/workspace/mount-test.txt"],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    combined_output = result.stdout + result.stderr
    assert test_content in combined_output, (
        f"Output should contain file content. Got:\n{combined_output}"
    )
