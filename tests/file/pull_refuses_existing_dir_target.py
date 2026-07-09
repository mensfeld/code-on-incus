"""
Test for coi file pull - an existing directory at the final target is refused.
"""

import os
import subprocess


def _write_sentinel(directory, name="sentinel.txt", content="must survive"):
    path = os.path.join(directory, name)
    with open(path, "w") as f:
        f.write(content)
    return path, content


def test_pull_refuses_existing_dir_target(coi_binary, workspace_dir):
    """
    Remote /tmp/data pulled to "." resolves to "./data";
    when ./data is already an existing directory,
    the pull must refuse, and must not delete anything.
    """
    target_dir = os.path.join(workspace_dir, "data")
    os.mkdir(target_dir)
    sentinel, content = _write_sentinel(target_dir)

    result = subprocess.run(
        [coi_binary, "file", "pull", "coi-no-such-container:/tmp/data", "."],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Pull onto an existing directory should fail"
    assert "already exists" in result.stderr, f"Error should be actionable. stderr: {result.stderr}"

    with open(sentinel) as f:
        assert f.read() == content, "Existing directory contents must be untouched"


def test_pull_recursive_refuses_existing_dir_target(coi_binary, workspace_dir):
    """Same refusal for -r when the exact final target is an existing directory."""
    target_dir = os.path.join(workspace_dir, "data")
    os.mkdir(target_dir)
    sentinel, content = _write_sentinel(target_dir)

    result = subprocess.run(
        [coi_binary, "file", "pull", "-r", "coi-no-such-container:/tmp/data", "."],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Recursive pull onto an existing directory should fail"
    assert "already exists" in result.stderr, f"Error should be actionable. stderr: {result.stderr}"

    with open(sentinel) as f:
        assert f.read() == content, "Existing directory contents must be untouched"
