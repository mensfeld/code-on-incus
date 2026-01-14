"""
Test for network isolation - restricted mode blocks cloud metadata endpoint.

Tests that:
1. Container cannot reach cloud metadata service at 169.254.169.254
2. Prevents cloud credential exfiltration
"""

import subprocess
import time


def test_restricted_blocks_metadata_endpoint(coi_binary, workspace_dir, cleanup_containers):
    """
    Test that restricted mode blocks access to cloud metadata endpoint.

    Flow:
    1. Start shell in background (default: restricted mode)
    2. Try to curl 169.254.169.254 (AWS/GCP/Azure metadata endpoint)
    3. Verify connection is blocked/rejected
    4. Cleanup container
    """
    # Start shell in background
    result = subprocess.run(
        [coi_binary, "shell", "--workspace", workspace_dir, "--background", "--debug"],
        capture_output=True,
        text=True,
        timeout=60,
    )

    assert result.returncode == 0, f"Shell should start successfully. stderr: {result.stderr}"

    # Give container time to fully start
    time.sleep(5)

    # Try to curl cloud metadata endpoint (should be blocked)
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "curl -s --connect-timeout 2 http://169.254.169.254/latest/meta-data/",
        ],
        capture_output=True,
        text=True,
        timeout=10,
    )

    # Should fail (non-zero exit code) or timeout
    assert result.returncode != 0, "Should not be able to reach metadata endpoint 169.254.169.254"
