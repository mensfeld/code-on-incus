"""
Test that coi update patterns --dry-run --source <url> includes the custom URL in output.
"""

import os
import subprocess


def test_update_patterns_dry_run_custom_source(coi_binary, tmp_path):
    """
    Dry-run with a custom --source URL includes that URL in the clone command.

    Flow:
    1. Pick a nonexistent target directory
    2. Run coi update patterns --dry-run --gtfobins-dir <path> --source <url>
    3. Verify exit code is 0
    4. Verify stdout contains the custom source URL
    """
    target_dir = str(tmp_path / "gtfobins-custom")
    custom_url = "https://github.com/example/custom-gtfobins.git"
    env = {**os.environ, "HOME": str(tmp_path)}

    result = subprocess.run(
        [
            coi_binary,
            "update",
            "patterns",
            "--dry-run",
            "--gtfobins-dir",
            target_dir,
            "--source",
            custom_url,
        ],
        capture_output=True,
        text=True,
        timeout=10,
        env=env,
    )

    assert result.returncode == 0, (
        f"Dry-run with custom source should succeed. stderr: {result.stderr}"
    )

    assert custom_url in result.stdout, (
        f"Custom source URL should appear in dry-run output. Got:\n{result.stdout}"
    )
