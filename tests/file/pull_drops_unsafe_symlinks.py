"""
Test that `coi file pull` does not recreate container symlinks whose target
escapes the pulled tree on the host.

Container content is untrusted: a symlink like `x -> /home/user/.ssh/...` or
`../../../../etc/shadow`, if recreated verbatim on the host, is a symlink-
extraction (Zip-Slip-class) host-tampering vector. Such symlinks must be dropped;
intra-tree relative links and regular files must survive.
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
        "ln -s real.txt /tmp/symtest/safe-link && "  # safe intra-tree link
        "ln -s /etc/passwd /tmp/symtest/abs-evil && "  # unsafe: absolute host path
        "ln -s ../../../../etc/shadow /tmp/symtest/sub/esc-evil"  # unsafe: escaping traversal
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
        # Safe content survives.
        assert os.path.exists(os.path.join(local_dir, "real.txt")), "regular file should be pulled"
        assert os.path.islink(os.path.join(local_dir, "safe-link")), (
            "safe intra-tree symlink should be preserved"
        )

        # Unsafe symlinks must NOT be recreated on the host (use lexists: check the
        # link itself, not its target).
        abs_evil = os.path.join(local_dir, "abs-evil")
        esc_evil = os.path.join(local_dir, "sub", "esc-evil")
        assert not os.path.lexists(abs_evil), (
            f"absolute-target symlink should have been dropped, but exists at {abs_evil}"
        )
        assert not os.path.lexists(esc_evil), (
            f"escaping-traversal symlink should have been dropped, but exists at {esc_evil}"
        )
    finally:
        subprocess.run(
            [coi_binary, "container", "delete", container_name, "--force"],
            capture_output=True,
            timeout=30,
        )
