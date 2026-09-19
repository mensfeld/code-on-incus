"""
Top-level [mounts] accepts the flat [[mounts]] array form end-to-end.

Before the config/profile mount-symmetry fix, top-level config only accepted the
nested `[[mounts.default]]` form while profiles only accepted the flat
`[[mounts]]` form. This test mounts a host dir into a real container via a flat
`[[mounts]]` block in the workspace `.coi/config.toml` and reads the file back —
proving the flat form now works at top-level scope through the real mount
pipeline (mirrors test_extra_dir_mount_readable, which uses the nested form).
"""

import subprocess
from pathlib import Path


def test_flat_mounts_toplevel_readable(coi_binary, cleanup_containers, workspace_dir, tmp_path):
    src = tmp_path / "flatsrc"
    src.mkdir()
    (src / "marker.txt").write_text("flat-mount-works")

    # Flat [[mounts]] form at top-level config (previously nested-only).
    coi_dir = Path(workspace_dir) / ".coi"
    coi_dir.mkdir(exist_ok=True)
    (coi_dir / "config.toml").write_text(
        f'[[mounts]]\nhost = "{src}"\ncontainer = "/home/code/.config/flat"\n'
    )

    result = subprocess.run(
        [coi_binary, "run", "--", "cat", "/home/code/.config/flat/marker.txt"],
        capture_output=True,
        text=True,
        timeout=120,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"stdout: {result.stdout}\nstderr: {result.stderr}"
    assert "flat-mount-works" in result.stdout, f"mounted file not read. Got:\n{result.stdout}"
