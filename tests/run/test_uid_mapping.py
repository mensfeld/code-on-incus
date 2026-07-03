"""
Issue #530: when the host uid differs from the container's code uid (1000) —
a macOS Colima/Lima user at 501, a CI runner at 1001 — the workspace must
still be writable by the container's code user. The run pipeline configures
raw.idmap for this; previously it did no UID mapping at all and writes failed.

These tests deliberately do NOT chmod the workspace: they rely on the UID
mapping being correct. On GitHub runners the workspace is owned by uid 1001
(!= 1000), so this is a genuine reproduction of #530, not a simulation.
"""

import os
import subprocess

import pytest


def test_run_writes_to_uid_mismatched_workspace(coi_binary, cleanup_containers, workspace_dir):
    """A command run via `coi run` can create + write files in a workspace
    owned by a non-1000 host uid, and the file appears on the host."""
    if os.getuid() == 1000:
        pytest.skip("host uid is 1000; no mismatch to exercise (raw.idmap not needed)")

    result = subprocess.run(
        [
            coi_binary,
            "run",
            "--workspace",
            workspace_dir,
            "--",
            "bash",
            "-c",
            "id -u > /workspace/uid.txt && echo mapped-write-ok > /workspace/marker.txt",
        ],
        capture_output=True,
        text=True,
        timeout=180,
    )

    combined = result.stdout + result.stderr
    assert result.returncode == 0, (
        f"writing to a uid-mismatched workspace must succeed (issue #530). Output:\n{combined}"
    )

    marker = os.path.join(workspace_dir, "marker.txt")
    assert os.path.exists(marker), "the container's write must be visible on the host mount"
    with open(marker) as f:
        assert f.read().strip() == "mapped-write-ok"

    # The in-container writer is the code user (uid 1000), proving the mapping
    # is what made the host-owned (non-1000) directory writable.
    uid_file = os.path.join(workspace_dir, "uid.txt")
    with open(uid_file) as f:
        assert f.read().strip() == "1000", (
            "the writer inside the container should be code (uid 1000)"
        )
