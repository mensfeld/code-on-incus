"""
Test for coi run - fixed timezone written to workspace file.

Tests that:
1. Run with --timezone set to a non-host timezone (Asia/Tokyo)
2. Container writes timezone abbreviation to a file in the mounted workspace
3. Host can read the file back and verify correct timezone (JST)
"""

import os
import subprocess


def test_run_timezone_fixed_workspace(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a fixed timezone is visible through workspace files on the host.

    Flow:
    1. Make workspace writable by container's code user (shift mapping)
    2. Run coi run --timezone Asia/Tokyo to write TZ to workspace file
    3. Read the file back on the host and verify JST
    """
    # Ensure workspace is writable by container's code user through shift mapping
    os.chmod(workspace_dir, 0o777)

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--timezone",
            "Asia/Tokyo",
            "--",
            "sh",
            "-c",
            "date +%Z > /workspace/tz_test.txt",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Write to workspace should succeed. stderr: {result.stderr}"

    tz_file = os.path.join(workspace_dir, "tz_test.txt")
    assert os.path.exists(tz_file), f"Timezone file should exist at {tz_file}"

    with open(tz_file) as f:
        file_content = f.read().strip()

    assert file_content == "JST", (
        f"Workspace file should contain JST for Asia/Tokyo, got: {file_content}"
    )
