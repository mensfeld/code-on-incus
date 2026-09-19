"""
Test that `coi run`'s boot-window egress block doesn't strand the container.

`coi run` now installs a temporary boot-block DROP rule in restricted/allowlist
mode (closing the pre-isolation boot window, matching `coi shell`), which the
apply-network phase must then remove. This asserts a restricted run still reaches
allowed egress afterwards — i.e. the boot block was installed AND cleanly removed,
not left stranding the container's network (#726 follow-up).
"""

import pathlib
import subprocess


def test_run_boot_block_does_not_strand_egress(coi_binary, cleanup_containers, workspace_dir):
    """Restricted coi run must still reach public internet after the boot block."""
    config_dir = pathlib.Path(workspace_dir) / ".coi"
    config_dir.mkdir(exist_ok=True)
    (config_dir / "config.toml").write_text('[network]\nmode = "restricted"\n')

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "curl",
            "-s",
            "--connect-timeout",
            "10",
            "http://example.com",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, (
        "restricted coi run should reach example.com (boot block removed cleanly). "
        f"stderr: {result.stderr}"
    )
    combined = result.stdout + result.stderr
    assert "Example Domain" in combined, f"Expected example.com HTML. Got:\n{combined}"
