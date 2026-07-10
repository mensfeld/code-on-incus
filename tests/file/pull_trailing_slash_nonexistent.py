"""
Test for coi file pull - trailing slash on a nonexistent destination fails fast.
"""

import os
import subprocess


def test_pull_trailing_slash_nonexistent(coi_binary, workspace_dir):
    """
    A trailing slash requires an existing directory;
    a nonexistent path must fail fast with a descriptive error
    before anything is transferred.

    The container name does not exist: a destination error
    (not a container error) proves validation happens first
    with zero side effects.
    """
    result = subprocess.run(
        [coi_binary, "file", "pull", "coi-no-such-container:/tmp/example.output", "newdir/"],
        capture_output=True,
        text=True,
        timeout=30,
        cwd=workspace_dir,
    )

    assert result.returncode != 0, "Pull to nonexistent 'newdir/' should fail"
    assert "does not exist" in result.stderr, (
        f"Error should be descriptive. stderr: {result.stderr}"
    )
    assert "trailing slash" in result.stderr, (
        f"Error should explain the rule. stderr: {result.stderr}"
    )
    assert "newdir" in result.stderr, f"Error should name the destination. stderr: {result.stderr}"

    assert not os.path.exists(os.path.join(workspace_dir, "newdir")), (
        "Nothing should be created for a rejected destination"
    )
