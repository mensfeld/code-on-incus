"""
Test for coi run - fixed timezone via stdout.

Tests that:
1. Run with --timezone set to a non-host timezone (Asia/Tokyo)
2. Verify container reports correct timezone abbreviation (JST) via stdout
"""

import subprocess


def test_run_timezone_fixed_stdout(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a fixed timezone is applied in the container.

    Flow:
    1. Run coi run --timezone Asia/Tokyo date +%Z
    2. Verify stdout contains JST
    """
    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--timezone",
            "Asia/Tokyo",
            "date",
            "+%Z",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    tz_abbrev = result.stdout.strip()
    assert tz_abbrev == "JST", (
        f"Timezone abbreviation should be JST for Asia/Tokyo, got: {tz_abbrev}"
    )
