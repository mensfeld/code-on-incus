"""
Test that `coi file pull` does not recreate container symlinks on the host.

Container content is untrusted: a symlink like `x -> /home/user/.ssh/...` or
`../../../../etc/shadow`, if recreated verbatim on the host, is a symlink-
extraction (Zip-Slip-class) host-tampering vector. Per-link target validation is
defeatable by chained symlinks, so ALL symlinks (and special files) are dropped
from pulled content; only regular files and directories survive.
"""

import os
import subprocess
import time

from support.helpers import calculate_container_name


def test_pull_drops_unsafe_symlinks(coi_binary, cleanup_containers, workspace_dir):
    container_name = calculate_container_name(workspace_dir, 1)

    result = subprocess.run(
        [coi_binary, "container", "launch", "coi-default", container_name],
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, f"Container launch should succeed. stderr: {result.stderr}"
    time.sleep(3)

    setup = (
        "mkdir -p /tmp/symtest/sub && "
        "echo content > /tmp/symtest/real.txt && "
        "echo nested > /tmp/symtest/sub/nested.txt && "
        "ln -s real.txt /tmp/symtest/safe-link && "  # intra-tree link (also dropped)
        "ln -s /etc/passwd /tmp/symtest/abs-evil && "  # unsafe: absolute host path
        "ln -s ../../../../etc/shadow /tmp/symtest/sub/esc-evil && "  # unsafe: escaping traversal
        "ln -s . /tmp/symtest/b && "  # chained-bypass component
        "ln -s b/../../../../etc/hosts /tmp/symtest/chain-evil"  # chained: escapes at runtime
    )
    result = subprocess.run(
        [coi_binary, "container", "exec", container_name, "--", "sh", "-c", setup],
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 0, f"Setup should succeed. stderr: {result.stderr}"

    local_dir = os.path.join(workspace_dir, "pulled")
    result = subprocess.run(
        [coi_binary, "file", "pull", "-r", f"{container_name}:/tmp/symtest", local_dir],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, f"Pull should succeed. stderr: {result.stderr}"

    try:
        # Regular files and directories survive (proves the pull actually ran).
        assert os.path.isfile(os.path.join(local_dir, "real.txt")), "regular file should be pulled"
        assert os.path.isfile(
            os.path.join(local_dir, "sub", "nested.txt")
        ), "nested regular file should be pulled"

        # Every symlink — safe, absolute, escaping, and chained — must be absent
        # on the host (use lexists: check the link itself, not its target). An
        # absolute symlink that incus dereferenced would land as a regular file of
        # the same name, so this also guards against silent dereferencing.
        for name in [
            "safe-link",
            "abs-evil",
            "b",
            "chain-evil",
            os.path.join("sub", "esc-evil"),
        ]:
            path = os.path.join(local_dir, name)
            assert not os.path.lexists(path), (
                f"symlink {name} should have been dropped, but exists at {path}"
            )
    finally:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
