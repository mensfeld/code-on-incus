"""
Test that `coi update patterns --dry-run` uses custom source URLs
configured in ~/.coi/config.toml under the [detection] section.
"""

import os
import subprocess


def test_update_patterns_dry_run_custom_source_via_config(coi_binary, tmp_path):
    """
    Source URLs are read from config, not CLI flags. A custom [detection]
    section in ~/.coi/config.toml changes which URL appears in dry-run output.
    """
    custom_gtfobins_url = "https://github.com/example/custom-gtfobins.git"
    custom_sigma_url = "https://github.com/example/custom-sigma.git"

    coi_dir = tmp_path / ".coi"
    coi_dir.mkdir()
    config_file = coi_dir / "config.toml"
    config_file.write_text(
        f'[detection]\n'
        f'gtfobins_source = "{custom_gtfobins_url}"\n'
        f'sigma_source = "{custom_sigma_url}"\n'
    )

    env = {**os.environ, "HOME": str(tmp_path)}

    result = subprocess.run(
        [coi_binary, "update", "patterns", "--dry-run"],
        capture_output=True,
        text=True,
        timeout=10,
        env=env,
    )

    assert result.returncode == 0, (
        f"Dry-run with custom config should succeed. stderr: {result.stderr}"
    )
    output = result.stdout
    assert custom_gtfobins_url in output, (
        f"Custom GTFOBins URL should appear in dry-run output. Got:\n{output}"
    )
    assert custom_sigma_url in output, (
        f"Custom Sigma URL should appear in dry-run output. Got:\n{output}"
    )
