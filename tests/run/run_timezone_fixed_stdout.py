"""
Test for coi run - fixed timezone via stdout.

Tests that:
1. Run with timezone config set to a non-host timezone (Asia/Tokyo)
2. Verify container reports correct timezone abbreviation (JST) via stdout
"""

import os
import subprocess
from pathlib import Path


def test_run_timezone_fixed_stdout(coi_binary, cleanup_containers, workspace_dir):
    """
    Test that a fixed timezone is applied in the container.

    Flow:
    1. Create .coi/config.toml with [timezone] mode="fixed", name="Asia/Tokyo"
    2. Run coi run date +%Z
    3. Verify stdout contains JST
    """
    # Create config with fixed timezone
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text("""
[timezone]
mode = "fixed"
name = "Asia/Tokyo"
""")

    env = os.environ.copy()
    env["COI_CONFIG"] = str(config_dir / "config.toml")

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "date",
            "+%Z",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        env=env,
    )

    assert result.returncode == 0, f"Run should succeed. stderr: {result.stderr}"

    tz_abbrev = result.stdout.strip()
    assert tz_abbrev == "JST", (
        f"Timezone abbreviation should be JST for Asia/Tokyo, got: {tz_abbrev}"
    )
