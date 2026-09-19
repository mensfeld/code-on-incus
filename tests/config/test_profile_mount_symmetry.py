"""
Config/profile mount-shape symmetry.

A `mounts` block must read identically in a profile and in top-level config: a
profile now accepts BOTH the flat `[[mounts]]` array form AND the nested
`[[mounts.default]]` form (the shape top-level [mounts] historically required).
These `coi profile info` tests are deterministic (no container) and assert both
shapes parse into the same mount, so the two scopes stay interchangeable and the
old flat form never regresses.
"""

import subprocess
from pathlib import Path

FLAT = """\
[container]
image = "coi-default"

[[mounts]]
host = "/host/sym"
container = "/mnt/sym"
"""

NESTED = """\
[container]
image = "coi-default"

[[mounts.default]]
host = "/host/sym"
container = "/mnt/sym"
"""


def _profile_info(coi_binary, workspace_dir, name, body):
    profile_dir = Path(workspace_dir) / ".coi" / "profiles" / name
    profile_dir.mkdir(parents=True)
    (profile_dir / "config.toml").write_text(body)
    result = subprocess.run(
        [coi_binary, "profile", "info", name, "--workspace", workspace_dir],
        capture_output=True,
        text=True,
        timeout=60,
        cwd=workspace_dir,
    )
    assert result.returncode == 0, f"profile info should succeed. stderr: {result.stderr}"
    return result.stdout + result.stderr


def test_profile_accepts_nested_mounts_default(coi_binary, cleanup_containers, workspace_dir):
    """The NEW capability: a profile accepts the nested [[mounts.default]] form."""
    out = _profile_info(coi_binary, workspace_dir, "nestedmnt", NESTED)
    assert 'host = "/host/sym"' in out, f"nested mount host not shown. Got:\n{out}"
    assert 'container = "/mnt/sym"' in out, f"nested mount container not shown. Got:\n{out}"


def test_profile_accepts_flat_mounts(coi_binary, cleanup_containers, workspace_dir):
    """Regression guard: the flat [[mounts]] form profiles always used still works."""
    out = _profile_info(coi_binary, workspace_dir, "flatmnt", FLAT)
    assert 'host = "/host/sym"' in out, f"flat mount host not shown. Got:\n{out}"
    assert 'container = "/mnt/sym"' in out, f"flat mount container not shown. Got:\n{out}"
