"""
Project (.coi/config.toml) config must not be able to weaken network isolation.

A repo-supplied or agent-planted project config that sets
block_metadata_endpoint=false / block_private_networks=false / mode=open is a
silent downgrade of the secure defaults. COI refuses such downgrades from
project scope (with a warning); only the user's ~/.coi/config.toml or an
explicit COI_CONFIG may relax them.
"""

import subprocess
from pathlib import Path


def test_project_config_network_downgrade_refused(coi_binary, cleanup_containers, workspace_dir):
    """A project config disabling the metadata/private-network blocks is ignored with a warning."""
    config_dir = Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text(
        "[network]\n"
        'mode = "restricted"\n'
        "block_metadata_endpoint = false\n"
        "block_private_networks = false\n"
    )

    result = subprocess.run(
        [coi_binary, "run", "--", "true"],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )

    combined = result.stdout + result.stderr
    assert "ignoring security-downgrading" in combined.lower(), (
        f"Expected a warning that the project network downgrade was ignored.\n"
        f"stdout: {result.stdout}\nstderr: {result.stderr}"
    )
    # Both downgrade flags should be named in the warnings.
    assert "block_metadata_endpoint" in combined, f"missing metadata warning:\n{combined}"
    assert "block_private_networks" in combined, f"missing private-networks warning:\n{combined}"
