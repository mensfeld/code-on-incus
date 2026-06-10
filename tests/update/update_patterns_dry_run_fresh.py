"""
Test that `coi update patterns --dry-run` on a fresh (nonexistent) HOME shows
both the GTFOBins clone command and the Sigma sparse-clone commands.
"""

import os
import subprocess


def test_update_patterns_dry_run_fresh_clone(coi_binary, tmp_path):
    """
    Dry-run with no existing databases shows clone commands for both GTFOBins
    and Sigma (sparse + sparse-checkout), and exits 0 without creating files.
    """
    env = {**os.environ, "HOME": str(tmp_path)}

    result = subprocess.run(
        [coi_binary, "update", "patterns", "--dry-run"],
        capture_output=True,
        text=True,
        timeout=10,
        env=env,
    )

    assert result.returncode == 0, f"Dry-run on fresh HOME should succeed. stderr: {result.stderr}"
    output = result.stdout
    assert "[dry-run]" in output, f"Expected '[dry-run]' marker. Got:\n{output}"
    # GTFOBins clone
    assert "clone" in output, f"Expected 'clone' command for GTFOBins. Got:\n{output}"
    # Sigma sparse-checkout step
    assert "sparse-checkout" in output, (
        f"Expected 'sparse-checkout' command for Sigma. Got:\n{output}"
    )
    # Both default dirs should appear
    gtfobins_dir = str(tmp_path / ".coi" / "gtfobins")
    sigma_dir = str(tmp_path / ".coi" / "sigma")
    assert gtfobins_dir in output, f"Expected GTFOBins dir in output. Got:\n{output}"
    assert sigma_dir in output, f"Expected Sigma dir in output. Got:\n{output}"
    # Dry-run must not create either directory
    assert not (tmp_path / ".coi" / "gtfobins").exists(), (
        "Dry-run must not create the GTFOBins directory"
    )
    assert not (tmp_path / ".coi" / "sigma").exists(), "Dry-run must not create the Sigma directory"
